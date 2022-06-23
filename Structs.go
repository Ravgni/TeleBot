package main

import "go.mongodb.org/mongo-driver/bson/primitive"

type UserStatus struct {
	UserID          int64
	ContactPending  bool
	GameNamePending bool
	GameName        string
}

type MongoUser struct {
	ID    int64                `bson:"_id,omitempty"`
	Name  string               `bson:"Name"`
	Games []primitive.ObjectID `bson:"Games"`
}

type MongoPlayerScore struct {
	Player     int64  `bson:"Player"`
	PlayerName string `bson:"PlayerName"`
	Score      int32  `bson:"Score, minsize"`
	WordNum    uint   `bson:"WordNum"`
}

type MongoGame struct {
	Players    []MongoPlayerScore `bson:"Players"`
	Leader     string             `bson:"Leader"`
	TotalScore int32              `bson:"TotalScore, minsize"`
	Name       string             `bson:"Name"`
}

type InviteQuery struct {
	Name string
	ID   int64
}

type GameQuery struct {
	Name string
}
