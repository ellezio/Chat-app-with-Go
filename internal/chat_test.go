package internal

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type event struct {
	t EventType
	d EventData
}

type mockClient struct {
	id     string
	events []event
}

func newMockClient(id string) *mockClient {
	return &mockClient{
		id:     id,
		events: make([]event, 0),
	}
}

func (mc *mockClient) GetId() string {
	return mc.id
}
func (mc *mockClient) HandleEvent(evt EventType, data EventData) {
	mc.events = append(mc.events, event{evt, data})
}

func TestChat_ClientConnectionFlow(t *testing.T) {
	cht := NewChat("test1", nil)
	cliId := "clientTest1"
	cli := newMockClient(cliId)

	cht.ConnectClient(cli)
	if c, ok := cht.connectedClients[cliId]; !ok {
		t.Errorf("ConnectClient: client '%s' has not been added to connected clients", cliId)
	} else if cl := c.(*mockClient); cl.id != cli.id {
		t.Errorf("ConnectClient: client '%s' differ between passed and stored in connected", cliId)
	}

	cht.DisconnectClient(cli)
	if c, ok := cht.disconnectedClients[cliId]; !ok {
		t.Errorf("DisconnectClient: client '%s' has not been added to disconnected clients", cliId)
	} else if cl := c.(*mockClient); cl.id != cli.id {
		t.Errorf("DisconnectClient: client '%s' differ between passed and stored in disconnected clients", cliId)
	} else if _, ok := cht.connectedClients[cliId]; ok {
		t.Errorf("DisconnectClient: client '%s' still in connected clinet after disconnecting", cliId)
	}

	cht.ConnectClient(cli)
	if c, ok := cht.connectedClients[cliId]; !ok {
		t.Errorf("ConnectClient: client '%s' has not been added to connected clients", cliId)
	} else if cl := c.(*mockClient); cl.id != cli.id {
		t.Errorf("ConnectClient: client '%s' differ between passed and stored in connected", cliId)
	} else if _, ok := cht.disconnectedClients[cliId]; ok {
		t.Errorf("ConnectClient: client '%s' still in disconnected clinet after connecting", cliId)
	}

	cht.RemoveClient(cli)
	if _, ok := cht.connectedClients[cliId]; ok {
		t.Errorf("RemoveClient: client '%s' has not been removed from connectedClients", cliId)
	}

	if _, ok := cht.disconnectedClients[cliId]; ok {
		t.Errorf("RemoveClient: client '%s' has not been removed from disconnectedClients", cliId)
	}
}

func TestChat_Broadcast(t *testing.T) {
	cht := NewChat("test1", nil)

	connectedClient := newMockClient("connectedClient")
	cht.ConnectClient(connectedClient)

	disconnectedClient := newMockClient("disconnectedClient")
	cht.ConnectClient(disconnectedClient)
	cht.DisconnectClient(disconnectedClient)

	evt := EventData{
		Msg:        &Message{},
		Cht:        cht,
		Connected:  false,
		OnlySender: false,
		SenderId:   "",
	}

	cht.Broadcast(Event_NewMessage, evt)

	if len(connectedClient.events) != 1 {
		t.Errorf("connected client recived %d messages expected 1", len(connectedClient.events))
	} else if evt := connectedClient.events[0]; !evt.d.Connected {
		t.Error("connected clinet recived message for disconnected client")
	}

	if len(disconnectedClient.events) != 1 {
		t.Errorf("disconnected client recived %d messages expected 1", len(disconnectedClient.events))
	} else if evt := disconnectedClient.events[0]; evt.d.Connected {
		t.Error("disconnected clinet recived message for connected client")
	}
}

func TestChatEvent_UnmarshalJSON(t *testing.T) {
	events := make([]ChatEvent, 0)
	cht := NewChat("testChat", nil)
	cht.publishEvent = func(event ChatEvent) error {
		events = append(events, event)
		return nil
	}

	chtevt := ChatEvent{
		Type:    Event_NewChat,
		ChatId:  cht.Id,
		UserId:  "senderId",
		Details: cht,
	}
	cht.publishEvent(chtevt)

	msg := New(cht.Id, "authorId", "test content", TextMessage)
	cht.NewMessage(msg, "authorId")
	cht.UpdateMessage(msg, "authortId")
	cht.SetHideMessage("msgId", "userId", true)
	cht.DeleteMessage("msgId")

	for _, evt := range events {
		jsonEvt, err := json.Marshal(evt)
		if err != nil {
			t.Fatal("failed to marshal event:", err)
		}

		recEvt := ChatEvent{}
		if err = recEvt.UnmarshalJSON(jsonEvt); err != nil {
			t.Fatal("failed to unmarshal event:", err)
		}

		originalEvtDetailsType := strings.ReplaceAll(reflect.TypeOf(evt.Details).String(), "*", "")
		receivedEvtDetailsType := strings.ReplaceAll(reflect.TypeOf(recEvt.Details).String(), "*", "")

		if originalEvtDetailsType != receivedEvtDetailsType {
			t.Fatalf("event details converted as %s expected %s", receivedEvtDetailsType, originalEvtDetailsType)
		}
	}
}
