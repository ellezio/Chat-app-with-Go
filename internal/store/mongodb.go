package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrParseID = errors.New("cannot parse id")
var ErrDecodeMessage = errors.New("cannot decode message")
var ErrDecodeChat = errors.New("cannot decode chat")

type Chat struct {
	ID   bson.ObjectID `bson:"_id"`
	Name string        `bson:"name"`
}

var client *mongo.Client

func InitConn() error {
	var err error

	uri := os.Getenv("MONGODB_URI")
	uri = fmt.Sprintf("mongodb://%s", uri)

	opts := options.Client().ApplyURI(uri)
	client, err = mongo.Connect(opts)

	initCacheConnection()

	return err
}

type MongodbStore struct{}

func (self *MongodbStore) getDatabase() *mongo.Database {
	return client.Database("chat_app")
}

func (self *MongodbStore) getMessagesCollection() *mongo.Collection {
	return self.getDatabase().Collection("messages")
}

func (self *MongodbStore) getChatsCollection() *mongo.Collection {
	return self.getDatabase().Collection("chats")
}

func (self *MongodbStore) SetHideMessage(id string, user string, value bool) (*internal.Message, error) {
	msgID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(ErrParseID, err)
	}

	coll := self.getMessagesCollection()

	var operation string
	if value {
		operation = "$addToSet"
	} else {
		operation = "$pull"
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": msgID},
		bson.M{operation: bson.M{"hidden_for": user}},
		opts,
	)

	var result internal.Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	cacheUpdateMessage(&result)

	return &result, nil
}

func (self *MongodbStore) DeleteMessage(id string) (*internal.Message, error) {
	msgID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(ErrParseID, err)
	}

	coll := self.getMessagesCollection()

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": msgID},
		bson.M{"$set": bson.M{"deleted": true}},
		opts,
	)

	var result internal.Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	cacheUpdateMessage(&result)

	return &result, nil
}

func (self *MongodbStore) GetMessage(msgID string) (*internal.Message, error) {
	id, err := bson.ObjectIDFromHex(msgID)
	if err != nil {
		return nil, errors.Join(ErrParseID, err)
	}

	coll := self.getMessagesCollection()

	var result internal.Message
	res := coll.FindOne(context.TODO(), bson.M{"_id": id})
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	return &result, nil
}

func (self *MongodbStore) GetMessages(chatID string) ([]*internal.Message, error) {
	msgs := cacheGetMessages(chatID)
	if len(msgs) > 0 {
		log.Println("CACHE HIT - get messages")
		return msgs, nil
	}

	id, err := bson.ObjectIDFromHex(chatID)
	if err != nil {
		return nil, errors.Join(ErrParseID, err)
	}

	coll := self.getMessagesCollection()
	var results []*internal.Message
	data, err := coll.Find(context.TODO(), bson.M{"chat_id": id})
	if err != nil {
		return nil, err
	}

	err = data.All(context.TODO(), &results)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	cachePopulateMessages(chatID, results)

	return results, nil
}

func (self *MongodbStore) SaveMessage(msg *internal.Message) error {
	if msg.ID == bson.NilObjectID {
		coll := self.getMessagesCollection()

		res, err := coll.InsertOne(
			context.TODO(),
			msg,
		)

		if err != nil {
			return err
		}

		if id, ok := res.InsertedID.(bson.ObjectID); ok {
			msg.ID = id
			cacheInsertMessage(msg)
		} else {
			return errors.New("failed to read inserted message ID")
		}
	} else {
		coll := self.getMessagesCollection()

		res, err := coll.UpdateByID(
			context.TODO(),
			msg.ID,
			bson.M{"$set": msg},
		)

		if err != nil {
			return err
		}

		if res.MatchedCount == 0 {
			return errors.New("update 0 messages")
		}

		cacheUpdateMessage(msg)
	}

	return nil
}

func (self *MongodbStore) UpdateMessageContent(id string, content string) (*internal.Message, error) {
	msgID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(ErrParseID, err)
	}

	coll := self.getMessagesCollection()

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": msgID},
		bson.M{"$set": bson.M{"content": content}},
		opts,
	)

	var result internal.Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	cacheUpdateMessage(&result)

	return &result, nil
}

// TODO: update on saving existing chat
func (self *MongodbStore) SaveChat(cht *internal.Chat) error {
	coll := self.getChatsCollection()

	res, err := coll.InsertOne(context.TODO(), cht)
	if err != nil {
		return errors.Join(errors.New("Failed to save chat"), err)
	}

	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		cht.ID = id.Hex()
	} else {
		return errors.New("failed to read inserted chat ID")
	}

	return nil
}

func (self *MongodbStore) GetChats() ([]*internal.Chat, error) {
	coll := self.getChatsCollection()

	res, err := coll.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil, errors.Join(errors.New("failed to get chats."), err)
	}

	var chts []Chat
	err = res.All(context.TODO(), &chts)
	if err != nil {
		return nil, errors.Join(ErrDecodeChat, err)
	}

	var results []*internal.Chat
	for _, cht := range chts {
		result := internal.NewChat(cht.Name, self)
		result.ID = cht.ID.Hex()
		results = append(results, result)
	}

	return results, nil
}
