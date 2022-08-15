package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/choseninlineresult"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/inlinequery"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var validResult = regexp.MustCompile(`(?s)СЛОВКО [\d]*.*./6.*https://slovko.zaxid.net/`)
var findWord = regexp.MustCompile(`[0-9]+`)
var findResult = regexp.MustCompile(`./6`)
var bot *gotgbot.Bot
var UserStatusC, GameStateC *mongo.Collection
var UserMap = make(map[int64]*UserStatus)

func main() {
	client, ctx := ConnectDB()
	defer client.Disconnect(ctx)

	/*
	 Get my collection instance
	*/
	UserStatusC = client.Database("BochkiDB").Collection("UserStatus")
	GameStateC = client.Database("BochkiDB").Collection("Games")

	token := os.Getenv("TELEGRAM_BOCHKI")
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	webhookDomain := os.Getenv("WEBHOOK_DOMAIN")
	webhookSecret := os.Getenv("WEBHOOK_SECRET")

	var err error
	bot, err = gotgbot.NewBot(token, &gotgbot.BotOpts{
		Client: http.Client{},
		DefaultRequestOpts: &gotgbot.RequestOpts{
			Timeout: gotgbot.DefaultTimeout,
			APIURL:  gotgbot.DefaultAPIURL,
		},
	})
	if err != nil {
		panic(err)
	}

	// Create updater and dispatcher.
	updater := ext.NewUpdater(&ext.UpdaterOpts{
		ErrorLog: nil,
		DispatcherOpts: ext.DispatcherOpts{
			// If an error is returned by a handler, log it and continue going.
			Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
				fmt.Println("an error occurred while handling update:", err.Error())
				return ext.DispatcherActionNoop
			},
			MaxRoutines: ext.DefaultMaxRoutines,
		},
	})
	dispatcher := updater.Dispatcher

	dispatcher.AddHandler(handlers.NewInlineQuery(inlinequery.All, ProcessQuery))
	dispatcher.AddHandler(handlers.NewChosenInlineResult(choseninlineresult.All, ProcessQueryResult))
	dispatcher.AddHandler(handlers.NewMessage(message.Command, ProcessCommand))
	dispatcher.AddHandler(handlers.NewMessage(message.Text, ProcessMessage))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.All, ProcessCallbackQuery))

	// Start the webhook server. We start the server before we set the webhook itself, so that when telegram starts
	// sending updates, the server is already ready.
	webhookOpts := ext.WebhookOpts{
		Listen:      "0.0.0.0", // This example assumes you're in a dev environment running ngrok on 8080.
		Port:        port,
		URLPath:     token,         // Using a secret (like the token) as the endpoint ensure that strangers aren't crafting fake updates.
		SecretToken: webhookSecret, // Setting a webhook secret (must be here AND in SetWebhook!) ensures that the webhook is set by you.
	}
	err = updater.StartWebhook(bot, webhookOpts)
	if err != nil {
		panic("failed to start webhook: " + err.Error())
	}

	// Get the full webhook URL that we are expecting to receive updates at.
	webhookURL := webhookOpts.GetWebhookURL(webhookDomain)

	// Tell telegram where they should send updates for you to receive them in a secure manner.
	_, err = bot.SetWebhook(webhookURL, &gotgbot.SetWebhookOpts{
		MaxConnections:     100,
		DropPendingUpdates: true,
		SecretToken:        webhookSecret, // The secret token passed at webhook start time.
	})
	if err != nil {
		panic("failed to set webhook: " + err.Error())
	}

	fmt.Printf("%s has been started...\n", bot.User.Username)

	// Idle, to keep updates coming in, and avoid bot stopping.
	updater.Idle()

	// bot.Debug = true

	// log.Printf("Authorized on account %s", bot.Self.UserName)

	// wh, err := tgbotapi.NewWebhook("https://bochki-bot.herokuapp.com:443/" + bot.Token)
	// if err != nil {
	// 	panic(err)
	// }

	// _, err = bot.Request(wh)
	// if err != nil {
	// 	panic(err)
	// }

	// info, err := bot.GetWebhookInfo()
	// if err != nil {
	// 	panic(err)
	// }

	// if !info.IsSet() && info.LastErrorDate != 0 {
	// 	log.Printf("failed to set webhook: %s", info.LastErrorMessage)
	// }

	// updates := bot.ListenForWebhook("/" + bot.Token)

	// go http.ListenAndServe(":"+port, nil)

	// for update := range updates {
	// 	if update.InlineQuery != nil {
	// 		ProcessQuery(update)
	// 	} else if update.ChosenInlineResult != nil {
	// 		ProcessQueryResult(update)
	// 	} else if update.Message != nil {
	// 		if update.Message.IsCommand() {
	// 			ProcessCommand(update)
	// 		} else {
	// 			ProcessMessage(update)
	// 		}
	// 	} else if update.CallbackQuery != nil {
	// 		ProcessCallbackQuery(update)
	// 	}
	// }
}

func GetUserStatus(ID int64) (*UserStatus, error) {
	if userStatus, ok := UserMap[ID]; !ok {
		if res := UserStatusC.FindOne(context.TODO(), bson.M{"_id": ID}); res.Err() == nil {
			userStatus = &UserStatus{UserID: ID}
			UserMap[ID] = userStatus
			return userStatus, nil
		} else {
			return nil, ErrGeneric
		}
	} else {
		return userStatus, nil
	}
}

func ProcessQuery(b *gotgbot.Bot, ctx *ext.Context) error {
	var results []gotgbot.InlineQueryResult

	if ctx.InlineQuery.Query == "Рахунок" {
		for key, value := range *GetScore(ctx.InlineQuery.From.Id) {
			article := NewInlineQueryResultArticle(uuid.New().String(), "Рахунок "+key, value)
			article.Description = "Показати рахунок"
			results = append(results, article)
		}
	} else if validResult.MatchString(ctx.InlineQuery.Query) {
		article1 := NewInlineQueryResultArticle(uuid.New().String()+"U", "Додати", ctx.InlineQuery.Query)
		article1.Description = "Додати словко"
		results = append(results, article1)
	}

	if results != nil {
		opts := gotgbot.AnswerInlineQueryOpts{CacheTime: 0, IsPersonal: true}
		if _, err := ctx.InlineQuery.Answer(b, results, &opts); err != nil {
			log.Println(err)
		}
	}

	return nil
}

func ProcessQueryResult(b *gotgbot.Bot, ctx *ext.Context) error {
	var reply string
	query := ctx.ChosenInlineResult.Query
	switch ctx.ChosenInlineResult.ResultId[len(ctx.ChosenInlineResult.ResultId)-1] {
	case 'U':
		reply = UpdateScore(ctx.ChosenInlineResult.From.Id, query)
	}

	SendMessage(ctx.ChosenInlineResult.From.Id, reply)

	return nil
}

func ProcessCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	from := ctx.Message.From
	UserID := from.Id
	var buttonRow []gotgbot.InlineKeyboardButton = nil
	var reply string
	switch ctx.Message.Text {
	case "start":
		filter := bson.M{"_id": UserID}
		update := bson.M{"$setOnInsert": MongoUser{ID: UserID, Name: from.FirstName + " " + from.LastName, Games: []primitive.ObjectID{}}}
		opts := options.Update().SetUpsert(true)
		if _, insertErr := UserStatusC.UpdateOne(context.TODO(), filter, update, opts); insertErr != nil {
			log.Print(insertErr)
		}
		UserMap[UserID] = &UserStatus{UserID: UserID}
		reply = "Тепер ви можете створити нову гру або долучитися до існуючої"
	case "invite":
		if status, err := GetUserStatus(UserID); err == nil {
			status.GameNamePending = true

			var games []bson.M

			matchstage := bson.D{{Key: "$match", Value: bson.M{"_id": UserID}}}
			unwindstage := bson.D{{Key: "$unwind", Value: "$Games"}}
			lookupstage := bson.D{{Key: "$lookup", Value: bson.M{
				"from":         "Games",
				"localField":   "Games",
				"foreignField": "_id",
				"as":           "Games"}}}
			projectstage := bson.D{{Key: "$project", Value: bson.M{"_id": 0, "GameID": "$Games._id", "Name": "$Games.Name"}}}

			if cur, err := UserStatusC.Aggregate(context.TODO(), mongo.Pipeline{matchstage, lookupstage, unwindstage, projectstage}); err == nil && cur.All(context.TODO(), &games) == nil {
				for _, game := range games {
					buttonRow = append(buttonRow, NewInlineKeyboardButton("Гра "+game["Name"].(string), "I "+game["GameID"].(primitive.ObjectID).Hex()))
				}
			}
			reply = "Виберіть існуючу гру або додайте нову"
		} else {
			reply = err.Error()
		}

	case "score":
		scores := GetScore(UserID)
		for key, value := range *scores {
			buttonRow = append(buttonRow, NewInlineKeyboardButton("Гра "+key, "S "+value))
		}
		if buttonRow != nil {
			reply = "Виберіть гру"
		} else {
			reply = "Немає початих ігор"
		}
	}

	SendMessage(UserID, reply, NewInlineKeyboardMarkup(buttonRow))

	return nil
}

func ProcessMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	var reply string
	From := ctx.Message.From

	if To := ctx.Message.Contact; To != nil {
		if status, err := GetUserStatus(To.UserId); err == nil {
			if !status.ContactPending {
				reply = "Спочатку викличте команду /invite"
			} else {
				if err := UserStatusC.FindOne(context.TODO(), bson.M{"_id": To.UserId}).Err(); err != nil {
					if err == mongo.ErrNoDocuments {
						reply = "Контакт не звертався до бота"
					}
				} else {
					SendInvite(From.FirstName+" "+From.LastName, From.Id, To.UserId)
					reply = "Запрошення відправлено"
				}
				status.ContactPending = false
			}
		} else {
			reply = err.Error()
		}
	} else if ctx.Message.ViaBot == nil && validResult.MatchString(ctx.Message.Text) {
		reply = UpdateScore(From.Id, ctx.Message.Text)
	} else if ctx.Message.Text != "" {
		if user, err := GetUserStatus(To.UserId); err == nil && user.GameNamePending {
			user.GameName = ctx.Message.Text
			user.GameNamePending = false
			user.ContactPending = true
			reply = "Додайте контакт"
		} else {
			reply = err.Error()
		}
	}
	SendMessage(ctx.Message.Chat.Id, reply)

	return nil
}

func SendInvite(fromName string, from, to int64) {
	inlineMarkup := NewInlineKeyboardMarkup(
		[]gotgbot.InlineKeyboardButton{
			NewInlineKeyboardButton("Прийняти", "A "+strconv.FormatInt(from, 10)),
			NewInlineKeyboardButton("Відхилити", "R "+strconv.FormatInt(from, 10)),
		})
	SendMessage(to, "Запрошення на гру від "+fromName, inlineMarkup)
}

func ProcessCallbackQuery(b *gotgbot.Bot, ctx *ext.Context) error {
	from := ctx.CallbackQuery.From
	data := ctx.CallbackQuery.Data
	switch data[0] {
	case 'A': //Accept
		toID, err := strconv.ParseInt(data[2:], 10, 0)
		if err != nil {
			panic(err)
		}
		var to MongoUser
		UserStatusC.FindOne(context.TODO(), bson.M{"_id": toID}).Decode(&to)
		AcceptInvite(from.Id, from.FirstName+" "+from.LastName, toID, to.Name, UserMap[toID].GameName)
	case 'R': //Reject
		ToID, err := strconv.ParseInt(data[2:], 10, 0)
		if err != nil {
			panic(err)
		}
		RejectInvite(from.FirstName+" "+from.LastName, ToID)
	case 'I': //Invite
		var game MongoGame
		if ID, err := primitive.ObjectIDFromHex(data[2:]); err == nil && GameStateC.FindOne(context.TODO(), bson.M{"_id": ID}).Decode(&game) == nil {
			user, _ := GetUserStatus(from.Id)
			user.GameName = game.Name
			user.GameNamePending = false
			user.ContactPending = true

			SendMessage(from.Id, "Додайте контакт")
		}
	case 'S': //Score
		SendMessage(from.Id, data[2:])
	}

	return nil
}

func AcceptInvite(from int64, fromName string, to int64, toName, gameName string) {
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
	var opts gotgbot.SendMessageOpts
	if text != "" {
		if len(inlineMarkup) > 0 {
			if markup, ok := inlineMarkup[0].(gotgbot.InlineKeyboardMarkup); ok {
				opts.ReplyMarkup = markup
			}
		}
		// Note that panics are a bad way to handle errors. Telegram can
		// have service outages or network errors, you should retry sending
		// messages or more gracefully handle failures.
		if _, err := bot.SendMessage(to, text, &opts); err != nil {
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
		return ErrGeneric.Error()
	} else {
		if User.Games == nil {
			return "Не має початих ігор"
		} else {
			filter := bson.M{"_id": bson.M{"$in": User.Games}}
			update := bson.M{
				"$inc": bson.M{"Players.$[elem].Score": score},
				"$set": bson.M{"Players.$[elem].WordNum": wordNum}}
			opts := options.Update().SetArrayFilters(options.ArrayFilters{Filters: bson.A{bson.M{"elem.Player": User.ID, "elem.WordNum": bson.M{"$lt": wordNum}}}})
			GameStateC.UpdateMany(context.TODO(), filter, update, opts)

			playerscore0 := BsonArrayValAt("Players", 0, "Score")
			playerscore1 := BsonArrayValAt("Players", 1, "Score")

			matchstage := bson.D{{Key: "$match", Value: bson.D{{Key: "_id", Value: bson.M{"$in": User.Games}}}}}
			sortstage := bson.D{{Key: "$addFields", Value: bson.M{
				"Players": bson.M{"$sortArray": bson.M{"input": "$Players", "sortBy": bson.M{"Score": -1}}}}}}
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

			pipeline := mongo.Pipeline{matchstage, sortstage, setleaderstage, mergestage}
			if cur, err := GameStateC.Aggregate(context.TODO(), pipeline); cur != nil && err == nil {
				return "Словко додано до рахунку"
			} else {
				return ErrGeneric.Error()
			}
		}
	}
}

func GetScore(from int64) *map[string]string {
	var ret map[string]string
	var results []bson.M

	matchstage := bson.D{{Key: "$match", Value: bson.M{"_id": from}}}
	unwindstage := bson.D{{Key: "$unwind", Value: "$Games"}}
	lookupstage := bson.D{{Key: "$lookup", Value: bson.M{
		"from":         "Games",
		"localField":   "Games",
		"foreignField": "_id",
		"as":           "Games"}}}
	projectstage := bson.D{{Key: "$project", Value: bson.M{
		"_id":        0,
		"Name":       "$Games.Name",
		"Leader":     "$Games.Leader",
		"TotalScore": "$Games.TotalScore"}}}

	if cur, err := UserStatusC.Aggregate(context.TODO(), mongo.Pipeline{matchstage, lookupstage, unwindstage, projectstage}); err == nil && cur.All(context.TODO(), &results) == nil {
		if resSize := len(results); resSize > 0 {
			ret = make(map[string]string, resSize)
			for _, result := range results {
				var ResultMessage string
				if result["TotalScore"].(int32) > 0 {
					ResultMessage = "Лідер " + result["Leader"].(string) + " з перевагою " + fmt.Sprint(result["TotalScore"])
				} else {
					ResultMessage = "Лідер відсутній"
				}
				ret[result["Name"].(string)] = ResultMessage
			}
		}
	} else if err != nil {
		log.Print(err)
	}
	return &ret
}
