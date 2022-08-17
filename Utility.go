package main

import "github.com/PaulSonOfLars/gotgbot/v2"

func NewInlineQueryResultArticle(Id string, Title string, Message string) gotgbot.InlineQueryResultArticle {
	return gotgbot.InlineQueryResultArticle{Id: Id, Title: Title, InputMessageContent: gotgbot.InputTextMessageContent{MessageText: Message}}
}

func NewInlineKeyboardButton(Text string, CallbackData string) gotgbot.InlineKeyboardButton {
	return gotgbot.InlineKeyboardButton{Text: Text, CallbackData: CallbackData}
}

func NewInlineKeyboardMarkup(buttonRow []gotgbot.InlineKeyboardButton) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{buttonRow}}
}
