package store

import (
	"context"
	"log"

	"github.com/ellezio/Chat-app-with-Go/internal"
	"github.com/redis/go-redis/v9"
)

/*

There are possible improvments to caching but I cannot find the way that
speaks to me the most, so I will go with very simple one just to grasp redis.

At this moment what most speaks to me is:
chat metadata + buckets
How I see it:
When messages are cache they land to bucket base of message order number (not id) in chat.
In bucket there will be let's say 100 messages and there will be 4 buckets. Message will
land in bucket (message's no.)/100 and when new bucket will be create the old one removed.
When message will be updated only one bucket will be invalidated and it will be ease to
find messages needed to fill it up when requested.
The one problem I can think of at this moment is requesting same bucket when it is not cached
every request will want to create sucha bucket but it can be solved by service that will
manage cache creation so when there is already request bucket creation other request will
listen for that bucket until it's created instead of creaing one.

Possible changes:

Discord uses so called "Buckets" to store message hot and cold, I may look into it.

1. Cache invalidaion:
Storing meta data of chats with last message id to decide
if message is in cache, where cached will be some amount of messages
in order to invalidate it. This requieres to change id
from randomly generated to ordered (snowflake id or just incrementing).

2. Data structure:
Ordered Set:
Scored by time or id (as described in Cache invalidation).
The score in redis i float64 but time is int64 it may cause ordering probelm when cast between them
Snowflake id can also cause this probelm. Incremental id to 53bit wouldn't cause any problems.

List:
List of messages would be easy to manage without problems with data type casting but requires
changes proposed in Cache Invalidation.
Another aproach would be storing only message ids in list and cached messages separately
this allow easy invalidation and even direct update (but I think invalidating would be safer)
but downside of it is more redis operation to perform

*/

var cache *redis.Client

func initCacheConnection() {
	cache = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		Protocol: 2,
	})
}

func cacheInsertMessage(msg *internal.Message) {
	k := "chat:" + msg.ChatId.Hex() + ":messages"
	r, err := cache.RPushX(context.Background(), k, msg).Result()
	if err != nil {
		log.Println(err)
		return
	}

	if r > 0 {
		cache.LTrim(context.Background(), k, 0, 99)
	}
}

func cacheUpdateMessage(msg *internal.Message) {
	k := "chat:" + msg.ChatId.Hex() + ":messages"
	cache.Del(context.Background(), k)
}

func cacheGetMessages(chatId string) []*internal.Message {
	k := "chat:" + chatId + ":messages"
	msgs := make([]*internal.Message, 0, 100)
	err := cache.LRange(context.Background(), k, 0, 99).ScanSlice(&msgs)
	if err != nil {
		log.Println(err)
		return nil
	}

	return msgs
}

func cachePopulateMessages(chatId string, msgs []*internal.Message) {
	k := "chat:" + chatId + ":messages"
	var items []any
	for _, msg := range msgs {
		items = append(items, msg)
	}

	err := cache.RPush(context.Background(), k, items...).Err()
	if err != nil {
		log.Println(err)
		return
	}
}
