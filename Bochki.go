package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
	port := os.Getenv("PORT")
	bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	wh, err := tgbotapi.NewWebhook("https://bochki-bot.herokuapp.com:443/" + bot.Token)
	if err != nil {
		panic(err)
	}

	_, err = bot.Request(wh)
	if err != nil {
		panic(err)
	}

	info, err := bot.GetWebhookInfo()
	if err != nil {
		panic(err)
	}

	if info.LastErrorDate != 0 {
		log.Printf("failed to set webhook: %s", info.LastErrorMessage)
	}

	updates := bot.ListenForWebhook("/" + bot.Token)

	go http.ListenAndServe(":"+port, nil)

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
		for key, value := range *GetScore(update.InlineQuery.From.ID) {
			article := tgbotapi.NewInlineQueryResultArticle(uuid.New().String(), "Рахунок "+key, value)
			article.Description = "Показати рахунок"
			options = append(options, article)
		}
	}

	if validResult.MatchString(update.InlineQuery.Query) {
		article1 := tgbotapi.NewInlineQueryResultArticle(uuid.New().String()+"U", "Додати", update.InlineQuery.Query)
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
	case 'U':
		reply = UpdateScore(update.ChosenInlineResult.From.ID, query)
	}

	SendMessage(update.ChosenInlineResult.From.ID, reply)
}

func ProcessCommand(update tgbotapi.Update) {
	from := update.Message.From
	UserID := from.ID
	var buttonRow []tgbotapi.InlineKeyboardButton
	var reply string
	switch update.Message.Command() {
	case "start":
		filter := bson.M{"_id": UserID}
		update := bson.M{"$setOnInsert": MongoUser{ID: UserID, Name: from.FirstName + " " + from.LastName, Games: []primitive.ObjectID{}}}
		opts := options.Update().SetUpsert(true)
		if _, insertErr := UserStatusC.UpdateOne(context.TODO(), filter, update, opts); insertErr != nil {
			log.Print(insertErr)
		}
		UserMap[UserID] = &UserStatus{UserID: UserID}
	case "invite":
		UserMap[UserID].GameNamePending = true

		var games []bson.D

		matchstage := bson.D{{Key: "$match", Value: bson.M{"_id": UserID}}}
		unwindstage := bson.D{{Key: "$unwind", Value: "$Games"}}
		lookupstage := bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "Games"},
			{Key: "localField", Value: "Games"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "Games"}}}}
		projectstage := bson.D{{Key: "$project", Value: bson.D{{Key: "_id", Value: 0}, {Key: "GameID", Value: "$Games._id"}, {Key: "Name", Value: "$Games.Name"}}}}

		if cur, err := UserStatusC.Aggregate(context.TODO(), mongo.Pipeline{matchstage, lookupstage, unwindstage, projectstage}); err == nil && cur.All(context.TODO(), &games) == nil {
			buttonRow = tgbotapi.NewInlineKeyboardRow()
			for _, game := range games {
				buttonRow = append(buttonRow, tgbotapi.NewInlineKeyboardButtonData("Гра "+game[1].Value.(string), "G "+game[0].Value.(primitive.ObjectID).Hex()))
			}
		}
		reply = "Виберіть існуючу гру або додайте нову"

	case "score":
		scores := GetScore(UserID)
		for key, value := range *scores {
			buttonRow = append(buttonRow, tgbotapi.NewInlineKeyboardButtonData("Гра "+key, "S "+value))
		}
		if len(*scores) > 0 {
			reply = "Виберіть гру"
		} else {
			reply = "Немає початих ігор"
		}
	}

	if buttonRow != nil {
		SendMessage(UserID, reply, tgbotapi.NewInlineKeyboardMarkup(buttonRow))
	} else {
		SendMessage(UserID, reply)
	}
}

func ProcessMessage(update tgbotapi.Update) {
	var reply string
	From := update.Message.From

	if To := update.Message.Contact; To != nil {
		if !UserMap[From.ID].ContactPending {
			reply = "Спочатку викличте команду /invite"
		} else {
			if err := UserStatusC.FindOne(context.TODO(), bson.M{"_id": To.UserID}).Err(); err != nil {
				if err == mongo.ErrNoDocuments {
					reply = "Контакт не звертався до бота"
				}
			} else {
				SendInvite(From.FirstName+" "+From.LastName, From.ID, To.UserID)
				reply = "Запрошення відправлено"
			}
			UserMap[From.ID].ContactPending = false
		}
	}

	if update.Message.ViaBot == nil && validResult.MatchString(update.Message.Text) {
		reply = UpdateScore(From.ID, update.Message.Text)
	}

	if user, ok := UserMap[From.ID]; ok && user.GameNamePending {
		user.GameName = update.Message.Text
		user.GameNamePending = false
		user.ContactPending = true
		reply = "Додайте контакт"
	}
	SendMessage(update.Message.Chat.ID, reply)
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
		toID, err := strconv.ParseInt(data[2:], 10, 0)
		if err != nil {
			panic(err)
		}
		var to MongoUser
		UserStatusC.FindOne(context.TODO(), bson.M{"_id": toID}).Decode(&to)
		AcceptInvite(from.ID, from.FirstName+" "+from.LastName, toID, to.Name, UserMap[toID].GameName)
	case 'R':
		ToID, err := strconv.ParseInt(data[2:], 10, 0)
		if err != nil {
			panic(err)
		}
		RejectInvite(from.FirstName+" "+from.LastName, ToID)
	case 'G':
		var game MongoGame
		if ID, err := primitive.ObjectIDFromHex(data[2:]); err == nil && GameStateC.FindOne(context.TODO(), bson.M{"_id": ID}).Decode(&game) == nil {
			user := UserMap[from.ID]
			user.GameName = game.Name
			user.GameNamePending = false
			user.ContactPending = true

			SendMessage(from.ID, "Додайте контакт")
		}
	case 'S':
		SendMessage(from.ID, data[2:])
	}
}

func AcceptInvite(from int64, fromName string, to int64, toName string, gameName string) {
	SendMessage(to, "Гравець "+fromName+" прийняв запрошення на гру")

	// TODO improve DB queries
	filter := bson.D{{Key: "Name", Value: gameName}, {Key: "Players.Player", Value: to}}
	update := bson.M{"$setOnInsert": MongoGame{Players: []MongoPlayerScore{{from, fromName, 0, 0}, {to, toName, 0, 0}}, Name: gameName}}
	opts := options.Update().SetUpsert(true)
	if GameId, insertErr := GameStateC.UpdateOne(context.TODO(), filter, update, opts); insertErr != nil {
		log.Print(insertErr)
	} else if GameId != nil {
		var DocID interface{}
		if GameId.UpsertedCount == 0 {
			if res := GameStateC.FindOne(context.TODO(), bson.D{{Key: "Name", Value: gameName}, {Key: "Players.Player", Value: to}, {Key: "Players.Player", Value: from}}); res.Err() == mongo.ErrNoDocuments {
				var Game bson.D
				GameStateC.FindOneAndUpdate(context.TODO(), filter, bson.M{"$push": bson.M{"Players": MongoPlayerScore{from, fromName, 0, 0}}}).Decode(&Game)
				DocID = Game[0].Value
				filter = bson.D{{Key: "_id", Value: from}}
			}
		} else {
			filter = bson.D{{Key: "_id", Value: bson.M{"$in": bson.A{from, to}}}}
			DocID = GameId.UpsertedID
		}
		update := bson.M{"$addToSet": bson.M{"Games": DocID}}
		UserStatusC.UpdateMany(context.TODO(), filter, update)
	}
}

func RejectInvite(fromName string, to int64) {
	SendMessage(to, "Гравець "+fromName+" відхилив запрошення на гру")
}

func SendMessage(to int64, text string, inlineMarkup ...interface{}) {
	if text != "" {
		msg := tgbotapi.NewMessage(to, text)
		if len(inlineMarkup) > 0 {
			if markup, ok := inlineMarkup[0].(tgbotapi.InlineKeyboardMarkup); ok {
				msg.ReplyMarkup = markup
			}
		}
		// Note that panics are a bad way to handle errors. Telegram can
		// have service outages or network errors, you should retry sending
		// messages or more gracefully handle failures.
		if _, err := bot.Send(msg); err != nil {
			panic(err)
		}
	}
}

func Score(in byte) byte {
	if in >= '1' && in <= '6' {
		return '6' - in + 1
	} else {
		return 0
	}
}

func UpdateScore(from int64, query string) string {
	wordNum, _ := strconv.Atoi(findWord.FindString(query))
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
			//TODO Bulk write
			filter := bson.M{"_id": bson.M{"$in": User.Games}}
			update := bson.M{
				"$inc": bson.M{"Players.$[elem].Score": score},
				"$set": bson.M{"Players.$[elem].WordNum": wordNum}}
			opts := options.Update().SetArrayFilters(options.ArrayFilters{Filters: bson.A{bson.M{"elem.Player": User.ID, "elem.WordNum": bson.M{"$lt": wordNum}}}})
			GameStateC.UpdateMany(context.TODO(), filter, update, opts)

			playerscore0 := BsonArrayValAt("Players", 0, "Score")
			playerscore1 := BsonArrayValAt("Players", 1, "Score")

			matchstage := bson.D{{Key: "$match", Value: bson.D{{Key: "_id", Value: bson.M{"$in": User.Games}}}}}
			//TODO switch to sortArray when Atlas ver >= 5.2
			unwindstage := bson.D{{Key: "$unwind", Value: "$Players"}}
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

			pipeline := mongo.Pipeline{matchstage, unwindstage, sortstage, groupstage, setleaderstage, mergestage}
			if cur, err := GameStateC.Aggregate(context.TODO(), pipeline); cur != nil && err == nil {
				return "Словко додано до рахунку"
			} else {
				return "Помилка"
			}
		}
	}
}

func GetScore(from int64) *map[string]string {
	var User MongoUser
	ret := make(map[string]string)
	if err = UserStatusC.FindOne(context.TODO(), bson.M{"_id": from}).Decode(&User); err == nil {
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
					ret[result.Name] = ResultMessage
				}
			}
		}
	}
	if err != nil {
		log.Print(err)
	}
	return &ret
}
