package constants

import (
	"nbot-wa/secrets"
	"nbot-wa/util"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func makeConst[T any](val T) func() T {
	return func() T {
		return val
	}
}

var (
	ChatIDBotTest = makeConst(util.PanicIfError(types.ParseJID(secrets.ChatIDBotTest)))
	ChatIDMe      = makeConst(util.PanicIfError(types.ParseJID(secrets.ChatIDMe)))
	ChatIDMinyan  = makeConst(util.PanicIfError(types.ParseJID(secrets.ChatIDMinyan)))

	ChatIDsToRead = makeConst([]types.JID{
		ChatIDBotTest(),
		ChatIDMe(),
		ChatIDMinyan(),
	})

	MinyanLocation = makeConst(util.PanicIfError(time.LoadLocation(secrets.MinyanTimezone)))
)

const (
	MinyanCalendarID     = secrets.MinyanCalendarID
	GoogleCalendarAPIKey = secrets.GoogleCalendarAPIKey

	MaintainerName = secrets.MaintainerName
)
