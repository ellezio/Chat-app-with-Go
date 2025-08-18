package database

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal/chat"
	"github.com/ellezio/Chat-app-with-Go/internal/message"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var client *mongo.Client

func NewDB() {
	var err error

	uri := os.Getenv("MONGODB_URI")
	uri = fmt.Sprintf("mongodb://%s", uri)

	opts := options.Client().ApplyURI(uri)
	client, err = mongo.Connect(opts)

	if err != nil {
		panic(err)
	}
}

func getDatabase() *mongo.Database {
	return client.Database("chat_app")
}

func getMessagesCollection() *mongo.Collection {
	return getDatabase().Collection("messages")
}

func getChatsCollection() *mongo.Collection {
	return getDatabase().Collection("chats")
}

func GetMessages(chatId bson.ObjectID) ([]*message.Message, error) {
	coll := getMessagesCollection()
	var results []*message.Message
	data, err := coll.Find(context.TODO(), bson.M{"chat_id": chatId})
	if err != nil {
		return nil, err
	}

	err = data.All(context.TODO(), &results)
	if err != nil {
		return nil, err
	}

	return results, nil
}

func GetMessage(id bson.ObjectID) (*message.Message, error) {
	coll := getMessagesCollection()

	var result message.Message
	res := coll.FindOne(context.TODO(), bson.M{"_id": id})
	err := res.Decode(&result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func SaveMessage(msg *message.Message) error {
	coll := getMessagesCollection()

	res, err := coll.InsertOne(
		context.TODO(),
		msg,
	)

	if err != nil {
		return err
	}

	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		msg.ID = id
	} else {
		return errors.New("Failed to cast InsertionID")
	}

	return nil
}

func UpdateStatus(id bson.ObjectID, status message.MessageStatus) error {
	coll := getMessagesCollection()

	res, err := coll.UpdateByID(
		context.TODO(),
		id,
		bson.D{bson.E{
			Key: "$set",
			Value: bson.D{
				bson.E{
					Key:   "status",
					Value: status,
				},
			},
		}},
	)

	if err != nil {
		return errors.Join(errors.New("Failed to update status"), err)
	}

	if res.ModifiedCount == 0 {
		fmt.Println(res)
		return errors.New("Failed to modify message")
	}

	return nil
}

func (self *MongodbStore) SetHideMessage(id bson.ObjectID, user string, value bool) (*message.Message, error) {
	coll := getMessagesCollection()

	var operation string
	if value {
		operation = "$addToSet"
	} else {
		operation = "$pull"
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": id},
		bson.M{operation: bson.M{"hidden_for": user}},
		opts,
	)

	var result message.Message
	err := res.Decode(&result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (self *MongodbStore) DeleteMessage(id bson.ObjectID) (*message.Message, error) {
	coll := getMessagesCollection()

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"deleted": true}},
		opts,
	)

	var result message.Message
	err := res.Decode(&result)

	if err != nil {
		fmt.Println(res)
		return nil, err
	}

	return &result, nil
}

func SaveChat(cht *chat.Chat) {

}

type MongodbStore struct{}

func (self *MongodbStore) GetMessage(msgID bson.ObjectID) (*message.Message, error) {
	return GetMessage(msgID)
}

func (self *MongodbStore) GetMessages(chatID bson.ObjectID) ([]*message.Message, error) {
	return GetMessages(chatID)
}

func (self *MongodbStore) SaveMessage(chatID bson.ObjectID, msg *message.Message) error {
	if msg.ID == bson.NilObjectID {
		return SaveMessage(msg)
	} else {
		coll := getMessagesCollection()

		res, err := coll.UpdateByID(
			context.TODO(),
			msg.ID,
			bson.M{"$set": msg},
		)

		if err != nil {
			return errors.Join(errors.New("Failed to update message"), err)
		}

		if res.ModifiedCount == 0 {
			fmt.Println(res)
			return errors.New("Failed to update message")
		}
	}

	return nil
}

func (self *MongodbStore) UpdateMessageContent(id bson.ObjectID, content string) (*message.Message, error) {
	coll := getMessagesCollection()

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"content": content}},
		opts,
	)

	var result message.Message
	err := res.Decode(&result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (self *MongodbStore) SaveChat(cht *chat.Chat) error {
	coll := getChatsCollection()

	res, err := coll.InsertOne(context.TODO(), cht)
	if err != nil {
		return errors.Join(errors.New("Failed to save chat"), err)
	}

	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		cht.ID = id
	} else {
		return errors.New("Failed to cast InsertionID")
	}

	return nil
}

func (self *MongodbStore) GetChats() ([]*chat.Chat, error) {
	coll := getChatsCollection()

	res, err := coll.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil, errors.Join(errors.New("Failed to get chats."), err)
	}

	var chts []*chat.Chat
	err = res.All(context.TODO(), &chts)
	if err != nil {
		return nil, errors.Join(errors.New("Failed to unmarshal chats."), err)
	}

	return chts, nil
}
