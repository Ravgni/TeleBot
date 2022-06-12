package main

import (
	"context"
	"fmt"
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
var UserMap map[int64]*UserStatus

type UserStatus struct {
	UserID          int64
	ContactPending  bool
	GameNamePending bool
	GameName        string
}

type MongoUser struct {
	ID    int64                `bson:"_id,omitempty"`
	Games []primitive.ObjectID `bson:"Games"`
}

type MongoPlayerScore struct {
	Player     int64  `bson:"Player"`
	PlayerName string `bson:"PlayerName"`
	Score      uint   `bson:"Score"`
	WordNum    uint   `bson:"WordNum"`
}

type MongoGame struct {
	Players    []MongoPlayerScore `bson:"Players"`
	Leader     string             `bson:"Leader"`
	TotalScore uint               `bson:"TotalScore"`
	Name       string             `bson:"Name"`
}

func main() {
	client, ctx := ConnectDB()
	defer client.Disconnect(ctx)

	/*
	 Get my collection instance
	*/
	UserStatusC = client.Database("BochkiDB").Collection("UserStatus")
	GameStateC = client.Database("BochkiDB").Collection("Games")

	UserMap = make(map[int64]*UserStatus)
	PopulateUserMap()

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

func PopulateUserMap() {
	var users []MongoUser
	if cur, err := UserStatusC.Find(context.TODO(), bson.M{}); cur != nil && err == nil && cur.All(context.TODO(), &users) == nil {
		for _, user := range users {
			UserMap[user.ID] = &UserStatus{UserID: user.ID}
		}
	}
}

func ProcessQuery(update tgbotapi.Update) {
	var options []interface{}

	if update.InlineQuery.Query == "Рахунок" {
		var User MongoUser
		if err = UserStatusC.FindOne(context.TODO(), bson.M{"_id": update.InlineQuery.From.ID}).Decode(&User); err == nil {
			if cur, err := GameStateC.Find(context.TODO(), bson.M{"_id": bson.M{"$in": User.Games}}); cur != nil && err == nil {
				var results []MongoGame
				if err = cur.All(context.TODO(), &results); err == nil {
					for _, result := range results {
						log.Print(result)
						var ResultMessage string
						if result.TotalScore > 0 {
							ResultMessage = "Лідер " + result.Leader + " з перевагою " + fmt.Sprint(result.TotalScore)
						} else {
							ResultMessage = "Лідер відсутній"
						}
						article := tgbotapi.NewInlineQueryResultArticle(uuid.New().String(), "Рахунок "+result.Name, ResultMessage)
						article.Description = "Показати рахунок"
						options = append(options, article)
					}
				}
			}
		}
		if err != nil {
			log.Print(err)
		}
	}

	if validResult.MatchString(update.InlineQuery.Query) {
		article1 := tgbotapi.NewInlineQueryResultArticle(uuid.New().String()+"S", "Додати", update.InlineQuery.Query)
		article1.Description = "Додати словко"
		options = append(options, article1)
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
		reply = UpdateScore(update.ChosenInlineResult.From.ID, query)
	}
	if reply != "" {
		SendMessage(update.ChosenInlineResult.From.ID, reply)
	}
}

func ProcessCommand(update tgbotapi.Update) {
	UserID := update.Message.From.ID
	switch update.Message.Command() {
	case "start":
		filter := bson.M{"_id": UserID}
		update := bson.M{"$setOnInsert": MongoUser{ID: UserID}}
		opts := options.Update().SetUpsert(true)
		if _, insertErr := UserStatusC.UpdateOne(context.TODO(), filter, update, opts); insertErr != nil {
			log.Print(insertErr)
		}
		UserMap[UserID] = &UserStatus{UserID: UserID}
	case "invite":
		UserMap[UserID].GameNamePending = true

		var buttonRow []tgbotapi.InlineKeyboardButton
		var games []bson.D

		matchstage := bson.D{{Key: "$match", Value: bson.M{"_id": UserID}}}
		unwindstage := bson.D{{Key: "$unwind", Value: "$Games"}}
		lookupstage := bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "Games"},
			{Key: "localField", Value: "Games"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "Games"}}}}
		addfieldstage := bson.D{{Key: "$addFields", Value: bson.M{"Name": "$Games.Name"}}}
		projectstage := bson.D{{Key: "$project", Value: bson.D{{Key: "_id", Value: 0}, {Key: "Name", Value: 1}}}}

		if cur, err := UserStatusC.Aggregate(context.TODO(), mongo.Pipeline{matchstage, lookupstage, unwindstage, addfieldstage, projectstage}); err == nil && cur.All(context.TODO(), &games) == nil {
			buttonRow = tgbotapi.NewInlineKeyboardRow()
			for _, game := range games {
				if gameName, ok := game[0].Value.(string); ok {
					buttonRow = append(buttonRow, tgbotapi.NewInlineKeyboardButtonData("Гра "+gameName, "G "+gameName))
					log.Print(game)
				}
			}
		}
		reply := "Виберіть існуючу гру або додайте нову"
		if buttonRow != nil {
			SendMessage(UserID, reply, tgbotapi.NewInlineKeyboardMarkup(buttonRow))
		} else {
			SendMessage(UserID, reply)
		}

	case "score":
	}
}

func ProcessMessage(update tgbotapi.Update) {
	var reply string
	From := update.Message.From

	//TODO optimize with map vs DB
	if To := update.Message.Contact; To != nil {
		if !UserMap[From.ID].ContactPending {
			reply = "Спочатку викличте команду /invite"
		} else {
			if err := UserStatusC.FindOne(context.TODO(), bson.M{"_id": /*To.UserID*/ From.ID}).Err(); err != nil {
				if err == mongo.ErrNoDocuments {
					reply = "Контакт не звертався до бота"
				}
			} else {
				SendInvite(From.FirstName+" "+From.LastName, From.ID, From.ID /*To.UserID*/)
				reply = "Запрошення відправлено"
			}
			UserMap[From.ID].ContactPending = false
		}
	}
	if validResult.MatchString(update.Message.Text) {
		reply = UpdateScore(From.ID, update.Message.Text)
	}

	if user, ok := UserMap[From.ID]; ok && user.GameNamePending {
		user.GameName = update.Message.Text
		user.GameNamePending = false
		user.ContactPending = true
		reply = "Додайте контакт"
	}
	if reply != "" {
		SendMessage(update.Message.Chat.ID, reply)
	}
}

func SendInvite(fromName string, from int64, to int64) {
	inlineMarkup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Прийняти", "A "+strconv.FormatInt(from, 10)),
			tgbotapi.NewInlineKeyboardButtonData("Відхилити", "R "+strconv.FormatInt(from, 10)),
		))
	SendMessage(to, "Запрошення на гру від "+fromName, inlineMarkup)
}

func ProcessCallbackQuery(update tgbotapi.Update) {
	from := update.CallbackQuery.From
	data := update.CallbackQuery.Data
	//TODO Regexp
	switch data[0] {
	case 'A':
		ToID, err := strconv.ParseInt(data[2:], 10, 0)
		if err != nil {
			panic(err)
		}
		//TODO Add ToName arg
		AcceptInvite(from.ID, from.UserName, ToID, "", UserMap[ToID].GameName)
	case 'R':
		ToID, err := strconv.ParseInt(data[2:], 10, 0)
		if err != nil {
			panic(err)
		}
		RejectInvite(from.FirstName, ToID)
	case 'G':
		if data != "" {
			user := UserMap[from.ID]
			user.GameName = data[2:]
			user.GameNamePending = false
			user.ContactPending = true
			SendMessage(from.ID, "Додайте контакт")
		}
	}
}

func AcceptInvite(from int64, fromName string, to int64, toName string, gameName string) {
	SendMessage(to, "Гравець "+fromName+"прийняв запрошення на гру")

	// TODO improve DB queries
	filter := bson.D{{Key: "Name", Value: gameName}, {Key: "Players.Player", Value: to}}
	update := bson.M{"$setOnInsert": MongoGame{Players: []MongoPlayerScore{{from, fromName, 0, 0}, {to, toName, 0, 0}}, Name: gameName}}
	opts := options.Update().SetUpsert(true)
	if GameId, insertErr := GameStateC.UpdateOne(context.TODO(), filter, update, opts); insertErr != nil {
		log.Print(insertErr)
	} else if GameId != nil {
		if GameId.UpsertedCount == 0 {
			if cur, err := GameStateC.Find(context.TODO(), bson.D{{Key: "Name", Value: gameName}, {Key: "Players.Player", Value: from}}); err == nil && cur.RemainingBatchLength() == 0 {
				GameStateC.UpdateOne(context.TODO(), filter, bson.M{"$push": bson.M{"Players": MongoPlayerScore{from, fromName, 0, 0}}})
				filter = bson.D{{Key: "_id", Value: from}}
			}
		} else {
			filter = bson.D{{Key: "$or", Value: bson.A{bson.M{"_id": from}, bson.M{"_id": to}}}}
		}
		update := bson.M{"$addToSet": bson.M{"Games": GameId.UpsertedID}}
		UserStatusC.UpdateMany(context.TODO(), filter, update)
	}
}

func RejectInvite(fromName string, to int64) {
	SendMessage(to, "Гравець "+fromName+"відхилив запрошення на гру")
}

func SendMessage(to int64, text string, inlineMarkup ...interface{}) {
	msg := tgbotapi.NewMessage(to, text)
	if len(inlineMarkup) > 0 {
		if markup, ok := inlineMarkup[0].(tgbotapi.InlineKeyboardMarkup); ok {
			msg.ReplyMarkup = markup
		}
	}
	// Note that panics are a bad way to handle errors. Telegram can
	// have service outages or network errors, you should retry sending
	// messages or more gracefully handle failures.
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

//TODO WordNum validation
func UpdateScore(from int64, query string) string {
	// wordNum, _ := strconv.Atoi(findWord.FindString(query))
	score := Score(findResult.FindString(query)[0])

	var User MongoUser
	if err := UserStatusC.FindOne(context.TODO(), bson.M{"_id": from}).Decode(&User); err != nil {
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
			setscorestage := bson.D{{Key: "$set", Value: bson.M{
				"Players.Score": bson.M{"$cond": bson.M{
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
