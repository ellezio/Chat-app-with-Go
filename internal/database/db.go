package database

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal/message"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var client *mongo.Client

func NewDB() {
	var (
		err error
	)

	uri := os.Getenv("MONGODB_URI")
	uri = fmt.Sprintf("mongodb://%s", uri)

	opts := options.Client().ApplyURI(uri)
	client, err = mongo.Connect(opts)

	if err != nil {
		panic(err)
	}
}

func GetMessages() ([]message.Message, error) {
	coll := client.Database("chat_app").Collection("messages")
	var results []message.Message
	data, err := coll.Find(context.TODO(), bson.D{})
	if err != nil {
		return nil, err
	}

	err = data.All(context.TODO(), &results)
	if err != nil {
		return nil, err
	}

	return results, nil
}

func SaveMessage(msg *message.Message) error {
	coll := client.Database("chat_app").Collection("messages")

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
	coll := client.Database("chat_app").Collection("messages")

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
