package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/lmatte7/gomesh"
	"github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/osutils"
	"github.com/taylorsreid/emradi/emradiprotos"
	_ "github.com/taylorsreid/emradi/server/migrations" // required to make pb aware that there might be migrations to run
	"google.golang.org/protobuf/proto"
)

const PORTNUM gomeshproto.PortNum = gomeshproto.PortNum_PRIVATE_APP // TODO: pick a unique port number

var app *pocketbase.PocketBase
var radio = gomesh.Radio{}
var radioRunning = false
var nodeInfo *gomeshproto.NodeInfo
var config *gomeshproto.Config
var myInfo *gomeshproto.MyNodeInfo
var messageId uint32
var channelIndex uint32

func main() {

	app = pocketbase.New()

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		// enable auto creation of migration files when making collection changes in the Dashboard
		// (the IsProbablyGoRun check is to enable it only during development)
		Automigrate: osutils.IsProbablyGoRun(),
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		logFile, err := os.OpenFile("server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			log.SetOutput(logFile)
		} // else we'll just write to stdErr

		err = startRadio()
		if err != nil {
			log.Println(err) // log but don't panic because server can still run without a radio
		}

		// routes in addition to standard CRUD routes
		se.Router.GET("/api/healthRadio", func(e *core.RequestEvent) error {
			return e.JSON(http.StatusOK, map[string]any{
				"myInfo": myInfo,
				"info":   nodeInfo,
				"config": config,
			})
		}) //.Bind(apis.RequireAuth()) // TODO: reenable auth for prod

		return se.Next()
	})

	// hook to restart the radio if selected db radio record is changed
	app.OnRecordAfterUpdateSuccess("radios").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("selected") {
			radio.Close()
			radioRunning = false
			err := startRadio()
			if err != nil {
				log.Println(err) // log but don't panic because server can still run without a radio
			}
		}
		return e.Next()
	})

	// Hook to send update via radio to users assigned to this incident. IP users will subscribe from the client side so no hook is necessary for them.
	app.OnRecordAfterUpdateSuccess("incidents").BindFunc(func(e *core.RecordEvent) error {
		// TODO:

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func startRadio() (err error) {

	// retrieve selected radio info from db
	radioRecord, err := app.FindFirstRecordByData("radios", "selected", true)
	if err != nil {
		return errors.New("WARNING: No selected radio found in database. No radio is currently running.")
	}

	// start the radio
	err = radio.Init(radioRecord.GetString("path"))
	if err != nil {
		return err
	}
	radioRunning = true

	// set channel Index
	channelIndex = uint32(radioRecord.GetInt("channelIndex"))

	// change the modem speed if set in db, otherwise continue with default
	if radioRecord.Get("modemMode") != nil {
		err = radio.SetModemMode(radioRecord.GetString("modemMode")) // don't return yet, we need the event handler to run
	}

	// start radio event handler
	go func() {
		for radioRunning {
			// fmt.Println("I'm a little teapot.")
			packets, err := radio.ReadResponse(false)

			for _, p := range packets {
				eventHandler(p)
			}

			// handle after packets since gomesh uses io.Reader.Read and docs say to handle data before errors
			if err != nil {
				log.Println(err) // just log it anyways since we're inside a goroutine
			}
		}
	}()

	// handle possible SetModemMode error AFTER the event handler starts
	if err != nil {
		return err
	}

	// we should just be able to call and forget since we already have a handler looping over radio.ReadResponse
	radio.GetRadioInfo()

	// radioResponses, err := radio.GetRadioInfo()

	// for _, rr := range radioResponses {
	// 	eventHandler(rr)
	// }
	// if err != nil { // handle after, same as above
	// 	return err
	// }

	return nil
}

func createOutgoingPacket(to uint32, payload []byte) *gomeshproto.ToRadio {
	toRadio := gomeshproto.ToRadio{
		PayloadVariant: &gomeshproto.ToRadio_Packet{
			Packet: &gomeshproto.MeshPacket{
				To:      to,
				WantAck: true,
				Id:      messageId,
				Channel: channelIndex,
				PayloadVariant: &gomeshproto.MeshPacket_Decoded{
					Decoded: &gomeshproto.Data{
						Payload: payload,
						Portnum: PORTNUM,
					},
				},
			},
		},
	}
	messageId++
	return &toRadio
}

func eventHandler(fromRadio *gomeshproto.FromRadio) {
	// if packet == nil { // TODO: do we need this? test it
	// 	return
	// }

	switch pv := fromRadio.GetPayloadVariant().(type) {
	case *gomeshproto.FromRadio_Packet:
		if d, ok := pv.Packet.GetPayloadVariant().(*gomeshproto.MeshPacket_Decoded); ok {

			//
			em := &emradiprotos.EmradiMessage{}
			err := proto.Unmarshal(d.Decoded.GetPayload(), em)
			if err != nil {
				log.Println(err)
				return
			}

			switch em.GetVariant().(type) {

			//
			case *emradiprotos.EmradiMessage_CreateIncident:

			//
			case *emradiprotos.EmradiMessage_CreateIncidentEvent:

				//
				i := em.GetCreateIncidentEvent()
				collection, err := app.FindCollectionByNameOrId("incidentEvents")
				if err != nil {
					log.Println(err)
					return
				}

				//
				record := core.NewRecord(collection)
				record.Set("description", i.Description)
				record.Set("notes", i.Notes)
				record.Set("created", i.Created)

				//
				u, err := app.FindFirstRecordByData("users", "meshAddress", d.Decoded.Source)
				if err != nil {
					log.Println(err)
					return
				}

				//
				record.Set("createdBy", u.Id)
				record.Set("affectedUser", u.Id)
				err = app.Save(record)
				if err != nil {
					log.Println(err)
				}

			//
			case *emradiprotos.EmradiMessage_ReadRequest:
				var recordBytes []byte
				request := em.GetReadRequest()
				if request.GetMultiple() {
					//
					record, err := app.FindRecordsByFilter(
						request.GetCollection(),
						request.GetFilter(),
						request.GetSort(),
						int(request.GetLimit()),
						int(request.GetOffset()),
					)
					if err != nil {
						// TODO:
					}

					//
					recordBytes, err = json.Marshal(record)
					if err != nil {
						// TODO:
					}
				} else {
					//
					record, err := app.FindFirstRecordByFilter(
						request.GetCollection(),
						request.GetFilter(),
					)
					if err != nil {
						// TODO:
					}

					//
					recordBytes, err = record.MarshalJSON()
					if err != nil {
						// TODO:
					}
				}

				//
				readResponse, err := proto.Marshal(&emradiprotos.ReadResponse{
					Response: recordBytes,
				})
				if err != nil {
					log.Println(err)
					return
				}

				//
				out, err := proto.Marshal(createOutgoingPacket(pv.Packet.From, readResponse))
				if err != nil {
					log.Println(err)
					return
				}
				err = radio.SendPacket(out)
				if err != nil {
					log.Println(err)
				}
			case *emradiprotos.EmradiMessage_UpdateIncident:

			}

		}
	case *gomeshproto.FromRadio_MyInfo:
		myInfo = pv.MyInfo
	case *gomeshproto.FromRadio_NodeInfo:
		nodeInfo = pv.NodeInfo
	case *gomeshproto.FromRadio_Config:
		config = pv.Config
	case *gomeshproto.FromRadio_LogRecord:
		log.Println(pv.LogRecord.String())
	case *gomeshproto.FromRadio_ConfigCompleteId:
	case *gomeshproto.FromRadio_Rebooted:
	case *gomeshproto.FromRadio_ModuleConfig:
	case *gomeshproto.FromRadio_Channel:
	case *gomeshproto.FromRadio_QueueStatus:
	case *gomeshproto.FromRadio_XmodemPacket:
	case *gomeshproto.FromRadio_Metadata:
	case *gomeshproto.FromRadio_MqttClientProxyMessage:
	default:

	}

}
