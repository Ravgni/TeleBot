package main

import "errors"

var ErrStartBot = errors.New("Спочатку ініціюйте бота (/start)")
var ErrGeneric = errors.New("Помилка")
