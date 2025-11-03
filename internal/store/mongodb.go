package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ellezio/Chat-app-with-Go/internal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrParseID = errors.New("cannot parse id")
var ErrDecodeMessage = errors.New("cannot decode message")
var ErrDecodeChat = errors.New("cannot decode chat")

type Chat struct {
	ID   bson.ObjectID `bson:"_id,omitempty"`
	Name string        `bson:"name"`
}

type Message struct {
	ID         bson.ObjectID          `bson:"_id,omitempty"`
	ChatID     bson.ObjectID          `bson:"chat_id"`
	AuthorID   bson.ObjectID          `bson:"author_id"`
	Content    string                 `bson:"content"`
	Type       internal.MessageType   `bson:"type"`
	CreatedAt  time.Time              `bson:"created_at"`
	ModifiedAt time.Time              `bson:"modified_at"`
	Status     internal.MessageStatus `bson:"status"`
	HiddenFor  []string               `bson:"hidden_for"`
	Deleted    bool                   `bson:"deleted"`
}

func (m *Message) fromInternal(msg *internal.Message) {
	var err error
	m.ID = msg.ID
	m.ChatID = msg.ChatID
	m.AuthorID, err = bson.ObjectIDFromHex(msg.AuthorID)
	if err != nil {
		log.Println(err)
	}
	m.Content = msg.Content
	m.Type = msg.Type
	m.CreatedAt = msg.CreatedAt
	m.ModifiedAt = msg.ModifiedAt
	m.Status = msg.Status
	m.HiddenFor = msg.HiddenFor
	m.Deleted = msg.Deleted
}

func (m *Message) toInternal(user internal.User) *internal.Message {
	return &internal.Message{
		ID:         m.ID,
		ChatID:     m.ChatID,
		AuthorID:   user.ID.Hex(),
		Content:    m.Content,
		Type:       m.Type,
		CreatedAt:  m.CreatedAt,
		ModifiedAt: m.ModifiedAt,
		Status:     m.Status,
		HiddenFor:  m.HiddenFor,
		Deleted:    m.Deleted,

		Author: user,
	}
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

func (ms *MongodbStore) getUsersCollection() *mongo.Collection {
	return ms.getDatabase().Collection("users")
}

func (self *MongodbStore) SetHideMessage(id string, userID string, value bool) (*internal.Message, error) {
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
		bson.M{operation: bson.M{"hidden_for": userID}},
		opts,
	)

	var result Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	user, err := self.GetUserByID(result.AuthorID.Hex())
	if err != nil {
		return nil, errors.Join(errors.New("failed to attache author to message"), err)
	}

	rmsg := result.toInternal(*user)

	cacheUpdateMessage(rmsg)

	return rmsg, nil
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

	var result Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	user, err := self.GetUserByID(result.AuthorID.Hex())
	if err != nil {
		return nil, errors.Join(errors.New("failed to attache author to message"), err)
	}

	rmsg := result.toInternal(*user)

	cacheUpdateMessage(rmsg)

	return rmsg, nil
}

func (self *MongodbStore) GetMessage(msgID string) (*internal.Message, error) {
	id, err := bson.ObjectIDFromHex(msgID)
	if err != nil {
		return nil, errors.Join(ErrParseID, err)
	}

	coll := self.getMessagesCollection()

	var result []struct {
		Message `bson:",inline"`
		Author  internal.User `bson:"author"`
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"_id": id}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "users",
			"localField":   "author_id",
			"foreignField": "_id",
			"as":           "author",
		}}},
		{{Key: "$unwind", Value: "$author"}},
	}

	res, err := coll.Aggregate(context.TODO(), pipeline)
	err = res.All(context.TODO(), &result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	rmsg := result[0].toInternal(result[0].Author)

	return rmsg, nil
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

	var results []struct {
		Message `bson:",inline"`
		Author  internal.User `bson:"author"`
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"chat_id": id}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "users",
			"localField":   "author_id",
			"foreignField": "_id",
			"as":           "author",
		}}},
		{{Key: "$unwind", Value: "$author"}},
	}

	data, err := coll.Aggregate(context.TODO(), pipeline)
	if err != nil {
		return nil, errors.Join(errors.New("failed to get mesages"), err)
	}

	err = data.All(context.TODO(), &results)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	rmsgs := make([]*internal.Message, 0, len(results))
	for _, result := range results {
		rmsg := result.toInternal(result.Author)
		rmsgs = append(rmsgs, rmsg)
	}

	cachePopulateMessages(chatID, rmsgs)

	return rmsgs, nil
}

func (self *MongodbStore) SaveMessage(m *internal.Message) error {
	user, err := self.GetUserByID(m.AuthorID)
	if err != nil {
		return errors.Join(errors.New("failed to get user for message"), err)
	}
	m.Author = *user

	msg := Message{}
	msg.fromInternal(m)

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
			m.ID = id
			cacheInsertMessage(m)
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

		cacheUpdateMessage(m)
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

	var result Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	user, err := self.GetUserByID(result.AuthorID.Hex())
	if err != nil {
		return nil, errors.Join(errors.New("failed to attache author to message"), err)
	}

	rmsg := result.toInternal(*user)

	cacheUpdateMessage(rmsg)

	return rmsg, nil
}

// TODO: update on saving existing chat
func (self *MongodbStore) SaveChat(cht *internal.Chat) error {
	coll := self.getChatsCollection()

	res, err := coll.InsertOne(context.TODO(), Chat{Name: cht.Name})
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

func (ms *MongodbStore) CreateUser(user *internal.User) error {
	coll := ms.getUsersCollection()

	res, err := coll.InsertOne(context.TODO(), user)
	if err != nil {
		return errors.Join(errors.New("failed to create user"), err)
	}

	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		user.ID = id
	} else {
		return errors.New("failed to read inserted user ID")
	}

	return nil
}

func (ms *MongodbStore) GetUser(name string) (*internal.User, error) {
	coll := ms.getUsersCollection()

	res := coll.FindOne(
		context.TODO(),
		bson.M{"name": name},
	)

	var user internal.User
	if err := res.Decode(&user); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to get user with name \"%s\"", name), err)
	}

	return &user, nil
}

func (ms *MongodbStore) GetUserByID(id string) (*internal.User, error) {
	coll := ms.getUsersCollection()

	uID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(errors.New("failed to parse user id"), err)
	}

	res := coll.FindOne(
		context.TODO(),
		bson.M{"_id": uID},
	)

	var user internal.User
	if err := res.Decode(&user); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to get user with id \"%s\"", id), err)
	}

	return &user, nil
}
