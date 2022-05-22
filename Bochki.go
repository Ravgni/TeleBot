package main

import (
	"fmt"
	"log"
	"os"
	"regexp"

	uuid "github.com/google/uuid"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var numericKeyboard = tgbotapi.NewInlineKeyboardMarkup(
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("1.com", "http://1.com"),
		tgbotapi.NewInlineKeyboardButtonData("2", "2"),
		tgbotapi.NewInlineKeyboardButtonData("3", "3"),
	))

func main() {
	token := os.Getenv("TELEGRAM_BOCHKI")
	fmt.Print(token)
	bot, err := tgbotapi.NewBotAPI(token)
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

	var validResult = regexp.MustCompile(`(?s)СЛОВКО [\d]*.*./6.*https://slovko.zaxid.net/`)
	// var wordNum = regexp.MustCompile(`[\d]`)
	// var resultScore = regexp.MustCompile(`./6`)

	// Let's go through each update that we're getting from Telegram.
	for update := range updates {
		if update.InlineQuery != nil {
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

			if _, err := bot.Request(inlineConf); err != nil {
				log.Println(err)
			}
		} else {
			var msg tgbotapi.MessageConfig

			if update.ChosenInlineResult != nil {
				msg = tgbotapi.NewMessage(update.ChosenInlineResult.From.ID, "Додати до рахунку")
			} else if update.Message == nil {
				// Telegram can send many types of updates depending on what your Bot
				// is up to. We only want to look at messages for now, so we can
				// discard any other updates.
				continue
			} else {
				var reply string

				if update.Message.ViaBot != nil && validResult.MatchString(update.Message.Text) {
					reply = "Додати до рахунку"
				} else {
					continue
				}

				// Now that we know we've gotten a new message, we can construct a
				// reply! We'll take the Chat ID and Text from the incoming message
				// and use it to create a new message.
				msg = tgbotapi.NewMessage(update.Message.Chat.ID, reply)
				// We'll also say that this message is a reply to the previous message.
				// For any other specifications than Chat ID or Text, you'll need to
				// set fields on the `MessageConfig`.
				msg.ReplyToMessageID = update.Message.MessageID
			}

			// Okay, we're sending our message off! We don't care about the message
			// we just sent, so we'll discard it.
			if _, err := bot.Send(msg); err != nil {
				// Note that panics are a bad way to handle errors. Telegram can
				// have service outages or network errors, you should retry sending
				// messages or more gracefully handle failures.
				panic(err)
			}
		}
	}
}
