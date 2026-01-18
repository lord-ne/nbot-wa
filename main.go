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
	"nbot-wa/util"

	"github.com/go-co-op/gocron/v2"
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
	Client                *whatsmeow.Client
	MessageQueue          chan MessageToSend
	CalendarEventsService *calendar.EventsService
	MinyanScheduler       gocron.Scheduler
}

func (state *ProgramState) HandleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Printf("Received message in '%v'\n", v.Info.Chat.String())

		if v.Info.IsFromMe {
			return
		}

		if !v.Info.IsGroup || slices.Contains(constants.ChatIDsToRead(), v.Info.Chat) {
			state.Client.MarkRead([]string{v.Info.ID}, time.Now(), v.Info.Chat, v.Info.Sender, types.ReceiptTypeRead)
		}

		if (v.Info.Chat == constants.ChatIDMe()) || (v.Info.Chat == constants.ChatIDBotTest()) {
			state.HandleDebugMessage(v)
		}

		if !v.Info.IsGroup || (v.Info.Chat == constants.ChatIDMinyan()) || (v.Info.Chat == constants.ChatIDBotTest()) {
			state.HandleMinyanMessage(v)
		}
	}
}

func (state *ProgramState) SetupEventHandler() {
	state.Client.AddEventHandler(func(evt interface{}) { state.HandleEvent(evt) })
}

func CreateAndSetupStandardProgramState() (*ProgramState, error) {
	ctx := context.Background()

	calendarBaseService := util.PanicIfError(calendar.NewService(
		ctx,
		option.WithAPIKey(constants.GoogleCalendarAPIKey)),
	)

	dbLog := waLog.Stdout("Database", "DEBUG", true)
	// Make sure you add appropriate DB connector imports, e.g. github.com/mattn/go-sqlite3 for SQLite as we did in this minimal working example
	container, err := sqlstore.New(ctx, "sqlite3", "file:secrets/wastore.db?_foreign_keys=on", dbLog)
	if err != nil {
		return nil, err
	}

	// If you want multiple sessions, remember their JIDs and use .GetDevice(jid) or .GetAllDevices() instead.
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, err
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	scheduler, err := gocron.NewScheduler(
		gocron.WithLocation(constants.MinyanLocation()),
		gocron.WithLimitConcurrentJobs(1, gocron.LimitModeWait))
	if err != nil {
		return nil, err
	}

	programState := &ProgramState{
		Client:                client,
		MessageQueue:          make(chan MessageToSend, 1000),
		CalendarEventsService: calendar.NewEventsService(calendarBaseService),
		MinyanScheduler:       scheduler,
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
                                pairingCode, err := client.PairPhone(context.Background(), constants.BotPhoneNumber, true, whatsmeow.PairClientFirefox, "Firefox (Linux)")
                                if err != nil {
                                    fmt.Println("Error generating pairing code: %v", err)
                                }  else {
                                    fmt.Println("Generated pairing code: %v", pairingCode)
                                }
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

	programState.MinyanScheduler.Start()

	return programState, nil
}

func (state *ProgramState) ReportErrorToMe(err error, errorLocation string) {
	errorMessage := fmt.Sprintf("Error in %s: '%s'", errorLocation, err.Error())
	fmt.Println(errorMessage)
	state.QueueSimpleStringMessage(constants.ChatIDMe(), fmt.Sprintf("```%s```", errorMessage))
}

func main() {
	programState := util.PanicIfError(CreateAndSetupStandardProgramState())

	time.Sleep(5 * time.Second)

	programState.QueueSimpleStringMessage(constants.ChatIDMe(), "```Bot started```")

	// Wait for Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	programState.Client.Disconnect()
}
