package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"nbot-wa/constants"
	"nbot-wa/daily"
	"nbot-wa/util"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type ProgramState struct {
	Client *whatsmeow.Client
	MessageQueue chan MessageToSend
	CalendarEventsService *calendar.EventsService
	DailyEventRunner *daily.DailyRunner
}

func (state *ProgramState) HandleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Printf("Received message in '%v'\n", v.Info.Chat.String())

		if slices.Contains(constants.ChatIDsToRead(), v.Info.Chat) {
			state.Client.MarkRead([]string{ v.Info.ID }, time.Now(), v.Info.Chat, v.Info.Sender, types.ReceiptTypeRead)
		}

		if (v.Info.Chat == constants.ChatIDMe()) || (v.Info.Chat == constants.ChatIDBotTest()) {
			state.HandleDebugMessage(v)
		}

		if (v.Info.Chat == constants.ChatIDMinyan()) {
			state.HandleMinyanMessage(v)
		}
	}
}

func (state *ProgramState) SetupEventHandler() {
	state.Client.AddEventHandler(func(evt interface{}) { state.HandleEvent(evt) })
}

func CreateAndSetupStandardProgramState() (*ProgramState, error) {
	calendarBaseService := util.PanicIfError(calendar.NewService(
		context.Background(),
		option.WithAPIKey(constants.GoogleCalendarAPIKey)),
	)

	dbLog := waLog.Stdout("Database", "DEBUG", true)
	// Make sure you add appropriate DB connector imports, e.g. github.com/mattn/go-sqlite3 for SQLite as we did in this minimal working example
	container, err := sqlstore.New("sqlite3", "file:secrets/wastore.db?_foreign_keys=on", dbLog)
	if err != nil {
		return nil, err
	}

	// If you want multiple sessions, remember their JIDs and use .GetDevice(jid) or .GetAllDevices() instead.
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		return nil, err
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	programState := &ProgramState{
		Client: client,
		MessageQueue: make(chan MessageToSend, 1000),
		CalendarEventsService: calendar.NewEventsService(calendarBaseService),
		DailyEventRunner: daily.MakeDailyRunner(5 * time.Minute),
	}

	programState.SetupMessageQueue()
	programState.SetupEventHandler()

	if client.Store.ID == nil {
		// No ID stored, new login
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			return nil, err
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				// Render the QR code here
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		// Already logged in, just connect
		err = client.Connect()
		if err != nil {
			return nil, err
		}
	}

	programState.RegisterDailyEvents()

	return programState, nil
}

func main() {

	programState := util.PanicIfError(CreateAndSetupStandardProgramState())

	time.Sleep(5 * time.Second)

	programState.QueueSimpleStringMessage(constants.ChatIDMe(), "*Bot started*")

	// Wait for Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	programState.Client.Disconnect()

}