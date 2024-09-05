package main

import (
	"nbot-wa/util"

	"go.mau.fi/whatsmeow/types/events"
)

func (state *ProgramState) HandleDebugMessage(v *events.Message) {
	if util.NormalizeString(v.Message.GetConversation()) == "ping" {
		state.QueueSimpleStringMessage(v.Info.Chat, "pong")
	}
}
