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
var bot *tgbotapi.BotAPI
var err error
var UserStatusC, GameStateC *mongo.Collection

type UserStatus struct {
	ID             int64                `bson:"_id,omitempty"`
	InvitePending  bool                 `bson:"InvitePending"`
	WaitForContact bool                 `bson:"WaitForContact"`
	Games          []primitive.ObjectID `bson:"Games"`
}

type PlayerScore struct {
	Player int64 `bson:"Player"`
	Score  uint  `bson:"Score"`
}

type Game struct {
	Players    []PlayerScore `bson:"PlayerScore"`
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

	// var wordNum = regexp.MustCompile(`[\d]`)
	// var resultScore = regexp.MustCompile(`./6`)

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
	return UserStatus{id, false, false, []primitive.ObjectID{}}
}

func ProcessQuery(update tgbotapi.Update) {
	var options []interface{}

	article := tgbotapi.NewInlineQueryResultArticle(uuid.New().String(), "Рахунок", "Рахунок 5-0")
	article.Description = "Показати рахунок"
	options = append(options, article)

	if validResult.MatchString(update.InlineQuery.Query) {
		article1 := tgbotapi.NewInlineQueryResultArticle(uuid.New().String(), "тест", update.InlineQuery.Query)
		article1.Description = "Показати тест"
		options = append(options, article1)
	}

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

func ProcessQueryResult(update tgbotapi.Update) {
	msg := tgbotapi.NewMessage(update.ChosenInlineResult.From.ID, "Додати до рахунку")

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
		AcceptInvite(From.UserName, From.ID, ToID)
	} else if update.CallbackQuery.Data[0] == 'R' {
		ToID, err := strconv.ParseInt(update.CallbackQuery.Data[2:len(update.CallbackQuery.Data)], 10, 0)
		if err != nil {
			panic(err)
		}
		RejectInvite(From.FirstName, ToID)
	}
}

func AcceptInvite(fromName string, from int64, to int64) {
	msg := tgbotapi.NewMessage(to, "Гравець "+fromName+"прийняв запрошення на гру")
	if _, err = bot.Send(msg); err != nil {
		panic(err)
	}

	if GameId, insertErr := GameStateC.InsertOne(context.TODO(), Game{Players: []PlayerScore{{from, 0}, {to, 0}}, TotalScore: 0}); insertErr != nil {
		log.Print(insertErr)
	} else if GameId != nil {
		filter := bson.D{{Key: "$or", Value: []interface{}{bson.D{{Key: "_id", Value: from}}, bson.D{{Key: "_id", Value: to}}}}}
		opts := bson.D{{Key: "$addToSet", Value: bson.D{{Key: "Games", Value: GameId.InsertedID}}}}
		UserStatusC.UpdateMany(context.TODO(), filter, opts)
	}
}

func RejectInvite(from string, to int64) {
	msg := tgbotapi.NewMessage(to, "Гравець "+from+"відхилив запрошення на гру")
	if _, err = bot.Send(msg); err != nil {
		panic(err)
	}
}
