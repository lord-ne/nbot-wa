package main

import (
	"context"
	"fmt"
	"nbot-wa/util"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type MessageToSend struct {
	Chat    types.JID
	Message *waE2E.Message
}

func (state *ProgramState) QueueMessage(chat types.JID, message *waE2E.Message) {
	fmt.Printf("Message queued in chat '%v' {%v}\n", chat.String(), message.String())
	state.MessageQueue <- MessageToSend{
		Chat:    chat,
		Message: message,
	}
}

func (state *ProgramState) QueueSimpleStringMessage(chat types.JID, message string) {
	state.QueueMessage(chat, &waE2E.Message{
		Conversation: proto.String(message),
	})
}

func (state *ProgramState) SetupMessageQueue() {
	go func() {
		for msg := range state.MessageQueue {
			delayTime := util.AsMilliseconds(util.RandBetween(500, 1000))
			time.Sleep(delayTime)

			typeTime := util.AsMilliseconds(util.RandBetween(1000, 2000))
			state.Client.SendChatPresence(msg.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
			time.Sleep(typeTime)

			state.Client.SendMessage(context.Background(), msg.Chat, msg.Message)

			// We won't process any more messages for at least 4 seconds
			rateLimitTime := util.AsMilliseconds(util.RandBetween(4000, 5000))
			time.Sleep(rateLimitTime)
		}
	}()
}
