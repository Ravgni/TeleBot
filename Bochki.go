package main

import (
	"context"
	"log"
	"os"
	"regexp"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var validResult = regexp.MustCompile(`(?s)СЛОВКО [\d]*.*./6.*https://slovko.zaxid.net/`)
var findWord = regexp.MustCompile(`[0-9]+`)
var findResult = regexp.MustCompile(`./6`)
var bot *tgbotapi.BotAPI
var err error
var UserStatusC, GameStateC *mongo.Collection

var ResultQueryID = uuid.New().String()

type UserStatus struct {
	ID             int64                `bson:"_id,omitempty"`
	WaitForContact bool                 `bson:"WaitForContact"`
	Games          []primitive.ObjectID `bson:"Games"`
}

type PlayerScore struct {
	Player     int64  `bson:"Player"`
	PlayerName string `bson:"PlayerName"`
	Score      uint   `bson:"Score"`
	WordNum    uint   `bson:"WordNum"`
}

type Game struct {
	Players    []PlayerScore `bson:"Players"`
	Leader     string        `bson:"Leader"`
	TotalScore uint          `bson:"TotalScore"`
}

func main() {
	client, ctx := ConnectDB()
	defer client.Disconnect(ctx)

	/*
	 Get my collection instance
	*/
	UserStatusC = client.Database("BochkiDB").Collection("UserStatus")
	GameStateC = client.Database("BochkiDB").Collection("Games")

	token := os.Getenv("TELEGRAM_BOCHKI")
	bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		panic(err)
	}

	bot.Debug = true

	// Create a new UpdateConfig struct with an offset of 0. Offsets are used
	// to make sure Telegram knows we've handled previous values and we don't
	// need them repeated.
	updateConfig := tgbotapi.NewUpdate(0)

	// Tell Telegram we should wait up to 30 seconds on each request for an
	// update. This way we can get information just as quickly as making many
	// frequent requests without having to send nearly as many.
	updateConfig.Timeout = 30

	// Start polling Telegram for updates.
	updates := bot.GetUpdatesChan(updateConfig)

	// Let's go through each update that we're getting from Telegram.
	for update := range updates {
		if update.InlineQuery != nil {
			ProcessQuery(update)
		} else if update.ChosenInlineResult != nil {
			ProcessQueryResult(update)
		} else if update.Message != nil {
			if update.Message.IsCommand() {
				ProcessCommand(update)
			} else {
				ProcessMessage(update)
			}
		} else if update.CallbackQuery != nil {
			ProcessCallbackQuery(update)
		}
	}
}

func NewUserStatus(id int64) UserStatus {
	return UserStatus{id, false, []primitive.ObjectID{}}
}

func ProcessQuery(update tgbotapi.Update) {
	var options []interface{}

	if update.InlineQuery.Query == "Рахунок" {
		article := tgbotapi.NewInlineQueryResultArticle(ResultQueryID, "Рахунок", "1111")
		article.Description = "Показати рахунок"
		options = append(options, article)
	}

	if validResult.MatchString(update.InlineQuery.Query) {
		article1 := tgbotapi.NewInlineQueryResultArticle(uuid.New().String()+"S", "Додати", update.InlineQuery.Query)
		article1.Description = "Додати словко"
		options = append(options, article1)
		// PendingQueries[]
	}

	if options != nil {
		inlineConf := tgbotapi.InlineConfig{
			InlineQueryID: update.InlineQuery.ID,
			IsPersonal:    true,
			CacheTime:     0,
			Results:       options,
		}

		if _, err = bot.Request(inlineConf); err != nil {
			log.Println(err)
		}
	}
}

func ProcessQueryResult(update tgbotapi.Update) {
	var reply string
	query := update.ChosenInlineResult.Query
	switch update.ChosenInlineResult.ResultID[len(update.ChosenInlineResult.ResultID)-1] {
	case 'S':
		word, _ := strconv.Atoi(findWord.FindString(query))
		score := Score(findResult.FindString(query)[0])
		reply = UpdateScore(update.ChosenInlineResult.From.ID, word, score)
	}
	msg := tgbotapi.NewMessage(update.ChosenInlineResult.From.ID, reply)

	if _, err = bot.Send(msg); err != nil {
		// Note that panics are a bad way to handle errors. Telegram can
		// have service outages or network errors, you should retry sending
		// messages or more gracefully handle failures.
		panic(err)
	}
}

func ProcessCommand(update tgbotapi.Update) {
	UserID := update.Message.From.ID
	var reply string
	switch update.Message.Command() {
	case "start":
		filter := bson.D{{Key: "_id", Value: UserID}}
		update := bson.D{{Key: "$setOnInsert", Value: NewUserStatus(UserID)}}
		opts := options.Update().SetUpsert(true)
		if _, insertErr := UserStatusC.UpdateOne(context.TODO(), filter, update, opts); insertErr != nil {
			log.Print(insertErr)
		}
	case "invite":
		reply = "Додайте контакт"
		//TODO optimize with map vs DB
		UserStatusC.UpdateByID(context.TODO(), UserID, bson.D{{Key: "$set", Value: bson.D{{Key: "WaitForContact", Value: true}}}})
	case "score":
	case "addWord":

	}
	if reply != "" {
		msg := tgbotapi.NewMessage(UserID, reply)

		if _, err = bot.Send(msg); err != nil {
			// Note that panics are a bad way to handle errors. Telegram can
			// have service outages or network errors, you should retry sending
			// messages or more gracefully handle failures.
			panic(err)
		}
	}
}

func ProcessMessage(update tgbotapi.Update) {
	var reply string
	From := update.Message.From

	//TODO optimize with map vs DB
	if To := update.Message.Contact; To != nil {
		if err := UserStatusC.FindOne(context.TODO(), bson.D{{Key: "_id", Value: From.ID}, {Key: "WaitForContact", Value: true}}).Err(); err != nil {
			if err == mongo.ErrNoDocuments {
				reply = "Спочатку викличте команду /invite"
			}
		} else {
			if err := UserStatusC.FindOne(context.TODO(), bson.D{{Key: "_id", Value: /*To.UserID*/ From.ID}}).Err(); err != nil {
				if err == mongo.ErrNoDocuments {
					reply = "Контакт не звертався до бота"
				}
			} else {
				SendInvite(From.FirstName+" "+From.LastName, From.ID /*To.UserID*/, From.ID)
				reply = "Запрошення відправлено"
			}
			UserStatusC.UpdateByID(context.TODO(), From.ID, bson.D{{Key: "$set", Value: bson.D{{Key: "WaitForContact", Value: false}}}})
		}
	}
	if reply != "" {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, reply)

		if _, err := bot.Send(msg); err != nil {
			panic(err)
		}
	}
}

func SendInvite(fromName string, from int64, to int64) {
	msg := tgbotapi.NewMessage(to, "Запрошення на гру від "+fromName)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Прийняти", "A "+strconv.FormatInt(from, 10)),
			tgbotapi.NewInlineKeyboardButtonData("Відхилити", "R "+strconv.FormatInt(from, 10)),
		))
	if _, err = bot.Send(msg); err != nil {
		panic(err)
	}
}

func ProcessCallbackQuery(update tgbotapi.Update) {
	From := update.CallbackQuery.From
	//TODO Regexp
	if update.CallbackQuery.Data[0] == 'A' {
		ToID, err := strconv.ParseInt(update.CallbackQuery.Data[2:len(update.CallbackQuery.Data)], 10, 0)
		if err != nil {
			panic(err)
		}
		AcceptInvite(From.ID, From.UserName, ToID)
	} else if update.CallbackQuery.Data[0] == 'R' {
		ToID, err := strconv.ParseInt(update.CallbackQuery.Data[2:len(update.CallbackQuery.Data)], 10, 0)
		if err != nil {
			panic(err)
		}
		RejectInvite(From.FirstName, ToID)
	}
}

func AcceptInvite(from int64, fromName string, to int64) {
	msg := tgbotapi.NewMessage(to, "Гравець "+fromName+"прийняв запрошення на гру")
	if _, err = bot.Send(msg); err != nil {
		panic(err)
	}

	if GameId, insertErr := GameStateC.InsertOne(context.TODO(), Game{Players: []PlayerScore{{from, fromName, 0, 0}, {to, "", 0, 0}}}); insertErr != nil {
		log.Print(insertErr)
	} else if GameId != nil {
		filter := bson.D{{Key: "$or", Value: []interface{}{bson.D{{Key: "_id", Value: from}}, bson.D{{Key: "_id", Value: to}}}}}
		update := bson.D{{Key: "$addToSet", Value: bson.D{{Key: "Games", Value: GameId.InsertedID}}}}
		UserStatusC.UpdateMany(context.TODO(), filter, update)
	}
}

func RejectInvite(from string, to int64) {
	msg := tgbotapi.NewMessage(to, "Гравець "+from+"відхилив запрошення на гру")
	if _, err = bot.Send(msg); err != nil {
		panic(err)
	}
}

func Score(in byte) int {
	if in >= '1' && in <= '6' {
		return int('1'-in) + 6
	} else {
		return 0
	}
}

func UpdateScore(from int64, wordNum int, score int) string {
	var User UserStatus
	if err := UserStatusC.FindOne(context.TODO(), bson.D{{Key: "_id", Value: from}}).Decode(&User); err != nil {
		if err == mongo.ErrNoDocuments {
			return "Спочатку ініціюйте бота (/start)"
		} else {
			return "Помилка"
		}
	} else {
		if User.Games == nil {
			return "Не має початих ігор"
		} else {
			playerscore0 := BsonArrayValAt("Players", 0, "Score")
			playerscore1 := BsonArrayValAt("Players", 1, "Score")

			matchstage := bson.D{{Key: "$match", Value: bson.M{"_id": bson.M{"$in": User.Games}}}}
			unwindstage := bson.D{{Key: "$unwind", Value: "$Players"}}
			setscorestage := bson.D{{Key: "$set", Value: bson.M{"Players.Score": bson.M{"$cond": bson.M{
				"if":   bson.M{"$eq": bson.A{"$Players.Player", from}},
				"then": bson.M{"$sum": bson.A{"$Players.Score", score}},
				"else": "$Players.Score"}}}}}
			sortstage := bson.D{{Key: "$sort", Value: bson.M{"_id": 1, "Players.Score": -1}}}
			groupstage := bson.D{{Key: "$group", Value: bson.M{
				"_id":        "$_id",
				"Players":    bson.M{"$push": "$Players"},
				"Leader":     bson.M{"$first": "$Leader"},
				"TotalScore": bson.M{"$first": "$TotalScore"}}}}
			setleaderstage := bson.D{{Key: "$set", Value: bson.M{
				"Leader": bson.M{"$cond": bson.M{
					"if":   bson.M{"$gt": bson.A{playerscore0, playerscore1}},
					"then": BsonArrayValAt("Players", 0, "PlayerName"),
					"else": "None"}},
				"TotalScore": bson.M{"$cond": bson.M{
					"if":   bson.M{"$gt": bson.A{playerscore0, playerscore1}},
					"then": bson.M{"$subtract": bson.A{playerscore0, playerscore1}},
					"else": 0}}}}}
			mergestage := bson.D{{Key: "$merge", Value: bson.M{"into": "Games", "on": "_id", "whenMatched": "merge", "whenNotMatched": "discard"}}}

			pipeline := mongo.Pipeline{matchstage, unwindstage, setscorestage, sortstage, groupstage, setleaderstage, mergestage}
			if cur, err := GameStateC.Aggregate(context.TODO(), pipeline); cur != nil && err == nil {
				return "Словко додано до рахунку"
			} else {
				return "Помилка"
			}
		}
	}
}

func BsonArrayValAt(arr string, idx uint, val string) bson.M {
	return bson.M{"$getField": bson.M{"field": val, "input": bson.M{"$arrayElemAt": bson.A{"$" + arr, idx}}}}
}
