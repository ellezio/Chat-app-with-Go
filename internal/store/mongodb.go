package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ellezio/Chat-app-with-Go/internal"
	"github.com/ellezio/Chat-app-with-Go/internal/config"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrParseId = errors.New("cannot parse id")
var ErrDecodeMessage = errors.New("cannot decode message")
var ErrDecodeChat = errors.New("cannot decode chat")
var ErrNoRecord = errors.New("record does't exist")

type Chat struct {
	Id   bson.ObjectID `bson:"_id,omitempty"`
	Name string        `bson:"name"`
}

type Message struct {
	Id         bson.ObjectID          `bson:"_id,omitempty"`
	ChatId     bson.ObjectID          `bson:"chatId"`
	AuthorId   bson.ObjectID          `bson:"authorId"`
	Content    string                 `bson:"content"`
	Type       internal.MessageType   `bson:"type"`
	CreatedAt  time.Time              `bson:"createdAt"`
	ModifiedAt time.Time              `bson:"modifiedAt"`
	Status     internal.MessageStatus `bson:"status"`
	HiddenFor  []string               `bson:"hiddenFor"`
	Deleted    bool                   `bson:"deleted"`
}

func (m *Message) fromInternal(msg *internal.Message) {
	var err error
	m.Id = msg.Id
	m.ChatId = msg.ChatId
	m.AuthorId, err = bson.ObjectIDFromHex(msg.AuthorId)
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
		Id:         m.Id,
		ChatId:     m.ChatId,
		AuthorId:   user.Id.Hex(),
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

type MongodbStore struct {
	cfg    config.MongoDB
	client *mongo.Client
	cache  *RedisStore
}

func NewMongodbStore(cfg config.MongoDB, cache *RedisStore) *MongodbStore {
	return &MongodbStore{cfg: cfg, cache: cache}
}

func (ms *MongodbStore) Connect() error {
	if ms.client != nil {
		err := ms.client.Ping(context.Background(), nil)
		if err == nil {
			return nil
		}

		ms.Disconnect()
	}

	var err error
	opts := options.Client().ApplyURI(ms.cfg.ConnectionString)
	ms.client, err = mongo.Connect(opts)
	return err
}

func (ms *MongodbStore) Disconnect() error {
	if ms.client == nil {
		return nil
	}

	err := ms.client.Disconnect(context.Background())
	ms.client = nil
	return err
}

func (ms *MongodbStore) getDatabase() (*mongo.Database, error) {
	if ms.client == nil {
		return nil, errors.New("MongoDB client not connected")
	}

	return ms.client.Database("chatApp"), nil
}

func (ms *MongodbStore) getMessagesCollection() (*mongo.Collection, error) {
	db, err := ms.getDatabase()
	return db.Collection("messages"), err
}

func (ms *MongodbStore) getChatsCollection() (*mongo.Collection, error) {
	db, err := ms.getDatabase()
	return db.Collection("chats"), err
}

func (ms *MongodbStore) getUsersCollection() (*mongo.Collection, error) {
	db, err := ms.getDatabase()
	return db.Collection("users"), err
}

func (ms *MongodbStore) SetHideMessage(id string, userId string, value bool) (*internal.Message, error) {
	coll, err := ms.getMessagesCollection()
	if err != nil {
		return nil, err
	}

	msgId, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(ErrParseId, err)
	}

	var operation string
	if value {
		operation = "$addToSet"
	} else {
		operation = "$pull"
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": msgId},
		bson.M{operation: bson.M{"hiddenFor": userId}},
		opts,
	)

	var result Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	user, err := ms.GetUserById(result.AuthorId.Hex())
	if err != nil {
		return nil, errors.Join(errors.New("failed to attache author to message"), err)
	}

	rmsg := result.toInternal(*user)

	ms.cache.UpdateMessage(rmsg)

	return rmsg, nil
}

func (ms *MongodbStore) DeleteMessage(id string) (*internal.Message, error) {
	coll, err := ms.getMessagesCollection()
	if err != nil {
		return nil, err
	}

	msgId, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(ErrParseId, err)
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": msgId},
		bson.M{"$set": bson.M{"deleted": true}},
		opts,
	)

	var result Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	user, err := ms.GetUserById(result.AuthorId.Hex())
	if err != nil {
		return nil, errors.Join(errors.New("failed to attache author to message"), err)
	}

	rmsg := result.toInternal(*user)

	ms.cache.UpdateMessage(rmsg)

	return rmsg, nil
}

func (ms *MongodbStore) GetMessage(msgId string) (*internal.Message, error) {
	coll, err := ms.getMessagesCollection()
	if err != nil {
		return nil, err
	}

	id, err := bson.ObjectIDFromHex(msgId)
	if err != nil {
		return nil, errors.Join(ErrParseId, err)
	}

	var result []struct {
		Message `bson:",inline"`
		Author  internal.User `bson:"author"`
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"_id": id}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "users",
			"localField":   "authorId",
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

func (ms *MongodbStore) GetMessages(chatId string) ([]*internal.Message, error) {
	coll, err := ms.getMessagesCollection()
	if err != nil {
		return nil, err
	}

	msgs := ms.cache.GetMessages(chatId)
	if len(msgs) > 0 {
		log.Println("CACHE HIT - get messages")
		return msgs, nil
	}

	id, err := bson.ObjectIDFromHex(chatId)
	if err != nil {
		return nil, errors.Join(ErrParseId, err)
	}

	var results []struct {
		Message `bson:",inline"`
		Author  internal.User `bson:"author"`
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"chatId": id}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "users",
			"localField":   "authorId",
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

	ms.cache.PopulateMessages(chatId, rmsgs)

	return rmsgs, nil
}

func (ms *MongodbStore) SaveMessage(m *internal.Message) error {
	coll, err := ms.getMessagesCollection()
	if err != nil {
		return err
	}

	user, err := ms.GetUserById(m.AuthorId)
	if err != nil {
		return errors.Join(errors.New("failed to get user for message"), err)
	}
	m.Author = *user

	msg := Message{}
	msg.fromInternal(m)

	if msg.Id == bson.NilObjectID {

		res, err := coll.InsertOne(
			context.TODO(),
			msg,
		)

		if err != nil {
			return err
		}

		if id, ok := res.InsertedID.(bson.ObjectID); ok {
			m.Id = id
			ms.cache.InsertMessage(m)
		} else {
			return errors.New("failed to read inserted message id")
		}
	} else {

		res, err := coll.UpdateByID(
			context.TODO(),
			msg.Id,
			bson.M{"$set": msg},
		)

		if err != nil {
			return err
		}

		if res.MatchedCount == 0 {
			return errors.New("update 0 messages")
		}

		ms.cache.UpdateMessage(m)
	}

	return nil
}

func (ms *MongodbStore) UpdateMessageContent(id string, content string) (*internal.Message, error) {
	coll, err := ms.getMessagesCollection()
	if err != nil {
		return nil, err
	}

	msgId, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(ErrParseId, err)
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	res := coll.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": msgId},
		bson.M{"$set": bson.M{"content": content}},
		opts,
	)

	var result Message
	err = res.Decode(&result)
	if err != nil {
		return nil, errors.Join(ErrDecodeMessage, err)
	}

	user, err := ms.GetUserById(result.AuthorId.Hex())
	if err != nil {
		return nil, errors.Join(errors.New("failed to attache author to message"), err)
	}

	rmsg := result.toInternal(*user)

	ms.cache.UpdateMessage(rmsg)

	return rmsg, nil
}

// TODO: update on saving existing chat
func (ms *MongodbStore) SaveChat(cht *internal.Chat) error {
	coll, err := ms.getChatsCollection()
	if err != nil {
		return err
	}

	res, err := coll.InsertOne(context.TODO(), Chat{Name: cht.Name})
	if err != nil {
		return errors.Join(errors.New("Failed to save chat"), err)
	}

	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		cht.Id = id.Hex()
	} else {
		return errors.New("failed to read inserted chat id")
	}

	return nil
}

func (ms *MongodbStore) GetChats() ([]*internal.Chat, error) {
	coll, err := ms.getChatsCollection()
	if err != nil {
		return nil, err
	}

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
		result := internal.NewChat(cht.Name, ms)
		result.Id = cht.Id.Hex()
		results = append(results, result)
	}

	return results, nil
}

func (ms *MongodbStore) CreateUser(user *internal.User) error {
	coll, err := ms.getUsersCollection()
	if err != nil {
		return err
	}

	res, err := coll.InsertOne(context.TODO(), user)
	if err != nil {
		return errors.Join(errors.New("failed to create user"), err)
	}

	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		user.Id = id
	} else {
		return errors.New("failed to read inserted user id")
	}

	return nil
}

func (ms *MongodbStore) GetUser(name string) (*internal.User, error) {
	coll, err := ms.getUsersCollection()
	if err != nil {
		return nil, err
	}

	res := coll.FindOne(
		context.TODO(),
		bson.M{"name": name},
	)

	var user internal.User
	if err := res.Decode(&user); err != nil {
		switch {
		case errors.Is(err, mongo.ErrNoDocuments):
			return nil, ErrNoRecord
		default:
			return nil, errors.Join(fmt.Errorf("failed to get user with name \"%s\"", name), err)
		}
	}

	return &user, nil
}

func (ms *MongodbStore) GetUserById(id string) (*internal.User, error) {
	if user := ms.cache.GetUser(id); user != nil {
		log.Println("User cache - HIT")
		return user, nil
	}
	log.Println("User cache - MISS")

	coll, err := ms.getUsersCollection()
	if err != nil {
		return nil, err
	}

	uid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.Join(errors.New("failed to parse user id"), err)
	}

	res := coll.FindOne(
		context.TODO(),
		bson.M{"_id": uid},
	)

	var user internal.User
	if err := res.Decode(&user); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to get user with id \"%s\"", id), err)
	}

	ms.cache.InsertUser(&user)

	return &user, nil
}
