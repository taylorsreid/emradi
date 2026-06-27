package main

import (
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/lmatte7/gomesh"
	"github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/osutils"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/taylorsreid/meshcad/meshcadprotos"
	_ "github.com/taylorsreid/meshcad/server/migrations" // required to make pb aware that there might be migrations to run
	"google.golang.org/protobuf/proto"
)

const PORTNUM gomeshproto.PortNum = gomeshproto.PortNum_PRIVATE_APP // TODO: pick a unique port number
const MAX_PAYLOAD int = 200                                         // TODO: ADJUST AS NECESSARY
const PACKET_OVERHEAD int = 12                                      // TODO: ADJUST AS NECESSARY

var app *pocketbase.PocketBase
var radio = gomesh.Radio{}
var radioRunning = false
var nodeInfo *gomeshproto.NodeInfo
var config *gomeshproto.Config
var myInfo *gomeshproto.MyNodeInfo
var channelIndex uint32

// reusable collections so we don't have to keep looking them up each time a function is called
var userCollection *core.Collection
var incidentCollection *core.Collection
var incidentEventCollection *core.Collection

// track used MeshPacket IDs
var packetId uint32
var usedMeshPacketIds = ttlcache.New(
	ttlcache.WithTTL[uint32, struct{}](30*time.Minute),
	ttlcache.WithCapacity[uint32, struct{}](10_000),
)

// track received and sent EmradiChunk IDs
var receivedChunks = ttlcache.New(
	ttlcache.WithCapacity[uint32, []*emradiprotos.EmradiChunk](10_000),
)
var sentChunks = ttlcache.New(
	ttlcache.WithCapacity[uint32, []*emradiprotos.EmradiChunk](1_000),
)

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

		// load up the reusable collection objects
		userCollection, err = app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		incidentCollection, err = app.FindCollectionByNameOrId("incidents")
		if err != nil {
			return err
		}
		incidentEventCollection, err = app.FindCachedCollectionByNameOrId("incidentEvents")
		if err != nil {
			return err
		}

		go usedMeshPacketIds.Start()

		err = startRadio()
		if err != nil {
			log.Println("FAILED TO START RADIO: " + err.Error()) // log but don't panic because server can still run without a radio
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
				log.Println("FAILED TO RESTART RADIO: " + err.Error()) // log but don't panic because server can still run without a radio
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

func startRadio() error {

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
				packetHandler(p)
			}

			// handle after packets since gomesh uses io.Reader.Read and docs say to handle data before errors
			if err != nil {
				log.Println("ERROR READING FROM RADIO: " + err.Error()) // just log it anyways since we're inside a goroutine
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

func getUnusedMeshPacketId() uint32 {
	r := rand.Uint32()
	if usedMeshPacketIds.Has(r) {
		return getUnusedMeshPacketId()
	}
	return r
}

func sendPayload(to uint32, convoId uint32, payload *emradiprotos.Payload) error {
	// make the payload into bytes
	bytes, err := proto.Marshal(payload)
	if err != nil {
		return err
	}

	// chunk the payload into maximum payload minus overhead byte chunks
	payloadChunks := slices.Collect(slices.Chunk(bytes, MAX_PAYLOAD-PACKET_OVERHEAD))
	// id := getUnusedMeshPacketId()

	// wrap the chunks in more protobufs
	for i := range payloadChunks {
		ec := &emradiprotos.EmradiChunk{
			Id:          convoId,                    // 4 bytes
			Numerator:   uint32(i + 1),              // 4 bytes
			Denominator: uint32(len(payloadChunks)), // 4 bytes
			Payload:     payloadChunks[i],
		}
		ecBytes, err := proto.Marshal(ec)
		if err != nil {
			return err
		}
		toRadio := gomeshproto.ToRadio{
			PayloadVariant: &gomeshproto.ToRadio_Packet{
				Packet: &gomeshproto.MeshPacket{
					To:      to,
					WantAck: true,
					Id:      packetId,
					Channel: channelIndex,
					PayloadVariant: &gomeshproto.MeshPacket_Decoded{
						Decoded: &gomeshproto.Data{
							Payload: ecBytes,
							Portnum: PORTNUM,
						},
					},
				},
			},
		}
		trb, err := proto.Marshal(&toRadio)
		if err != nil {
			return err
		}
		packetId++
		err = radio.SendPacket(trb)
		if err != nil {
			return err
		}
		if !sentChunks.Has(convoId) {
			sentChunks.Set(convoId, make([]*emradiprotos.EmradiChunk, 1), ttlcache.DefaultTTL)
		}
		sentChunks.Set(convoId, append(sentChunks.Get(convoId).Value(), ec), ttlcache.DefaultTTL)
	}
	return nil
}

func packetHandler(fromRadio *gomeshproto.FromRadio) {
	// if fromRadio == nil { // TODO: do we need this? test it
	// 	return
	// }

	switch pv := fromRadio.GetPayloadVariant().(type) {
	case *gomeshproto.FromRadio_Packet:
		if d, ok := pv.Packet.GetPayloadVariant().(*gomeshproto.MeshPacket_Decoded); ok {

			// unmarshal the raw bytes into one of our app's chunks, disregard the packet if it's not one of ours
			chunk := &emradiprotos.EmradiChunk{}
			err := proto.Unmarshal(d.Decoded.GetPayload(), chunk)
			if err != nil {
				log.Println("UNMARSHALING ERROR - EMRADICHUNK: " + err.Error()) // it's probably not an EmradiChunk, log it for now
				return
			}

			// create slice for ID if it doesn't exist already and add it
			// else if add chunk to slice if it's not already there (check by numerator, since we could get the same chunk multiple times and we don't want duplicates in the buffer)
			// else return because it's a duplicate chunk
			if !receivedChunks.Has(chunk.Id) {
				receivedChunks.Set(chunk.Id, []*emradiprotos.EmradiChunk{chunk}, ttlcache.DefaultTTL)
			} else if slices.IndexFunc(receivedChunks.Get(chunk.Id).Value(), func(c *emradiprotos.EmradiChunk) bool { return c.Numerator == chunk.Numerator }) == -1 {
				receivedChunks.Set(chunk.Id, append(receivedChunks.Get(chunk.Id).Value(), chunk), ttlcache.DefaultTTL)
			} else {
				return
			}

			// check if we may have all chunks according to the len of the buffer compared to the denominator.
			if uint32(len(receivedChunks.Get(chunk.Id).Value())) < chunk.Denominator {
				return // not all chunks received yet
			}

			// // check for missing chunks by iterating from 1 to denominator and checking if a chunk with that numerator exists in the buffer.
			// for i := int32(1); i <= chunk.Denominator; i++ {
			// 	if slices.IndexFunc(chunksBuffer[chunk.Id], func(c *emradiprotos.EmradiChunk) bool { return c.Numerator == i }) == -1 {
			// 		// Chunk i is missing, return for now. Meshtastic *SHOULD* retransmit automatically.
			// 		return
			// 	}
			// }

			// sort the slice of chunks by numerator
			asSlice := receivedChunks.Get(chunk.Id).Value()
			slices.SortFunc(asSlice, func(a *emradiprotos.EmradiChunk, b *emradiprotos.EmradiChunk) int {
				if a.Numerator < b.Numerator {
					return -1
				} else if a.Numerator > b.Numerator {
					return 1
				}
				return 0
			})

			// make it into one big byte array and remove it from the cache
			bytes := make([]byte, 0, len(asSlice)*MAX_PAYLOAD)
			for i := uint32(1); i <= chunk.Denominator; i++ {
				bytes = append(bytes, asSlice[i].Payload...)
			}
			receivedChunks.Delete(chunk.Id)

			payload := &emradiprotos.Payload{}
			err = proto.Unmarshal(bytes, payload)
			if err != nil {
				log.Println("UNMARSHALING ERROR - PAYLOAD: " + err.Error())
				return
			}

			switch request := payload.GetPayload().(type) {
			case *emradiprotos.Payload_Response:
				// TODO: why would this be sent to the server? should we ignore???
			case *emradiprotos.Payload_CreateUser:
				user := core.NewRecord(userCollection)
				user.SetEmail(request.CreateUser.Email)
				user.SetPassword(request.CreateUser.Password)
				user.Set("name", request.CreateUser.Name)

				err := app.Save(user)
				if err != nil {
					errStr := err.Error()
					log.Println("ERROR CREATING USER: " + errStr)
					err := sendPayload(fromRadio.Id, chunk.Id, &emradiprotos.Payload{
						Payload: &emradiprotos.Payload_Response{
							Response: &emradiprotos.Response{
								Status:  400, // TODO: determine what type of error it is
								Payload: &errStr,
							},
						},
					})
					if err != nil {
						log.Println("SEND ERROR: " + err.Error())
					}
					return
				}

				err = sendPayload(fromRadio.Id, chunk.Id, &emradiprotos.Payload{
					Payload: &emradiprotos.Payload_Response{
						Response: &emradiprotos.Response{
							Status:  201,
							Payload: &user.Id,
						},
					},
				})
				if err != nil {
					log.Println("SEND ERROR: " + err.Error())
					return
				}
			case *emradiprotos.Payload_CreateIncident:
				// TODO: add authorization here

				incident := core.NewRecord(incidentCollection)
				incident.Set("incidentType", request.CreateIncident.IncidentType)
				incident.Set("coordinates", types.GeoPoint{Lat: *request.CreateIncident.Latitude, Lon: *request.CreateIncident.Longitude})
				incident.Set("address", request.CreateIncident.Address)
				incident.Set("sentAt", payload.Timestamp)

				createdBy, err := app.FindFirstRecordByData(userCollection, "meshAddress", pv.Packet.From)
				if err != nil {
					errStr := fmt.Sprintf("ERROR CREATING INCIDENT, COULD NOT FIND USER ASSOCIATED WITH MESH ADDRESS \"%d\": %s", pv.Packet.From, err.Error())
					log.Println(errStr)
					err := sendPayload(pv.Packet.From, chunk.Id, &emradiprotos.Payload{
						Payload: &emradiprotos.Payload_Response{
							Response: &emradiprotos.Response{
								Status:  404,
								Payload: &errStr,
							},
						},
					})
					if err != nil {
						log.Println("SEND ERROR: " + err.Error())
					}
					return
				}
				incident.Set("createdBy", createdBy)

				err = app.Save(incident)
				if err != nil {
					errStr := err.Error()
					log.Println("ERROR CREATING INCIDENT: " + errStr)
					err := sendPayload(pv.Packet.From, chunk.Id, &emradiprotos.Payload{
						Payload: &emradiprotos.Payload_Response{
							Response: &emradiprotos.Response{
								Status:  400,
								Payload: &errStr,
							},
						},
					})
					if err != nil {
						log.Println("SEND ERROR: " + err.Error())
					}
					return
				}

				err = sendPayload(fromRadio.Id, chunk.Id, &emradiprotos.Payload{
					Payload: &emradiprotos.Payload_Response{
						Response: &emradiprotos.Response{
							Status:  201,
							Payload: &incident.Id,
						},
					},
				})
				if err != nil {
					log.Println("SEND ERROR: " + err.Error())
					return
				}
			case *emradiprotos.Payload_CreateIncidentEvent:
				// TODO: add authorization here

				ie := core.NewRecord(incidentEventCollection)
				ie.Set("title", request.CreateIncidentEvent.Title)
				ie.Set("details", request.CreateIncidentEvent.Details)
				ie.Set("sentAt", payload.Timestamp)
				ie.Set("affectedUser", request.CreateIncidentEvent.AffectedUser)

				createdBy, err := app.FindFirstRecordByData(userCollection, "meshAddress", pv.Packet.From)
				if err != nil {
					errStr := fmt.Sprintf("ERROR CREATING INCIDENT, COULD NOT FIND USER ASSOCIATED WITH MESH ADDRESS \"%d\": %s", pv.Packet.From, err.Error())
					log.Println(errStr)
					err := sendPayload(pv.Packet.From, chunk.Id, &emradiprotos.Payload{
						Payload: &emradiprotos.Payload_Response{
							Response: &emradiprotos.Response{
								Status:  404,
								Payload: &errStr,
							},
						},
					})
					if err != nil {
						log.Println("SEND ERROR: " + err.Error())
					}
					return
				}
				ie.Set("createdBy", createdBy)

				err = app.Save(ie)
				if err != nil {
					errStr := err.Error()
					log.Println("ERROR CREATING INCIDENTEVENT: " + errStr)
					err := sendPayload(pv.Packet.From, chunk.Id, &emradiprotos.Payload{
						Payload: &emradiprotos.Payload_Response{
							Response: &emradiprotos.Response{
								Status:  400,
								Payload: &errStr,
							},
						},
					})
					if err != nil {
						log.Println("SEND ERROR: " + err.Error())
					}
					return
				}

				err = sendPayload(pv.Packet.From, chunk.Id, &emradiprotos.Payload{
					Payload: &emradiprotos.Payload_Response{
						Response: &emradiprotos.Response{
							Status:  201,
							Payload: &ie.Id,
						},
					},
				})
				if err != nil {
					log.Println("SEND ERROR: " + err.Error())
					return
				}
			case *emradiprotos.Payload_Read:
				if request.Read.Multiple {
					results, err := app.FindRecordsByFilter(
						request.Read.Collection.String(),
						request.Read.Filter,
						*request.Read.Sort,
						int(*request.Read.Limit),
						int(*request.Read.Offset),
					)

					return
				}

			case *emradiprotos.Payload_UpdateUser:
			case *emradiprotos.Payload_UpdateIncident:
			case *emradiprotos.Payload_UpdateIncidentEvent:

			}

			// i := em.GetCreateIncidentEvent()
			// collection, err := app.FindCollectionByNameOrId("incidentEvents")
			// if err != nil {
			// 	log.Println(err.Error())
			// 	return
			// }

			// //
			// record := core.NewRecord(collection)
			// record.Set("description", i.Description)
			// record.Set("notes", i.Notes)
			// record.Set("created", i.Created)

			// //
			// u, err := app.FindFirstRecordByData("users", "meshAddress", d.Decoded.Source)
			// if err != nil {
			// 	log.Println(err.Error())
			// 	return
			// }

			// //
			// record.Set("createdBy", u.Id)
			// record.Set("affectedUser", u.Id)
			// err = app.Save(record)
			// if err != nil {
			// 	log.Println(err.Error())
			// }

			// case *emradiprotos.EmradiMessage_ReadRequest:
			// 	var recordBytes []byte
			// 	request := em.GetReadRequest()
			// 	if request.GetMultiple() {
			// 		//
			// 		record, err := app.FindRecordsByFilter(
			// 			request.GetCollection(),
			// 			request.GetFilter(),
			// 			request.GetSort(),
			// 			int(request.GetLimit()),
			// 			int(request.GetOffset()),
			// 		)
			// 		if err != nil {
			// 			// TODO:
			// 		}

			// 		//
			// 		recordBytes, err = json.Marshal(record)
			// 		if err != nil {
			// 			// TODO:
			// 		}
			// 	} else {
			// 		//
			// 		record, err := app.FindFirstRecordByFilter(
			// 			request.GetCollection(),
			// 			request.GetFilter(),
			// 		)
			// 		if err != nil {
			// 			// TODO:
			// 		}

			// 		//
			// 		recordBytes, err = record.MarshalJSON()
			// 		if err != nil {
			// 			// TODO:
			// 		}
			// 	}

			// 	//
			// 	readResponse, err := proto.Marshal(&emradiprotos.ReadResponse{
			// 		Response: recordBytes,
			// 	})
			// 	if err != nil {
			// 		log.Println(err.Error())
			// 		return
			// 	}

			// 	//
			// 	out, err := proto.Marshal(createOutgoingPacket(pv.Packet.From, readResponse))
			// 	if err != nil {
			// 		log.Println(err.Error())
			// 		return
			// 	}
			// 	err = radio.SendPacket(out)
			// 	if err != nil {
			// 		log.Println(err.Error())
			// 	}

		}
	case *gomeshproto.FromRadio_MyInfo:
		myInfo = pv.MyInfo
	case *gomeshproto.FromRadio_NodeInfo:
		nodeInfo = pv.NodeInfo
	case *gomeshproto.FromRadio_Config:
		config = pv.Config
	case *gomeshproto.FromRadio_LogRecord:
		log.Println("LOG: " + pv.LogRecord.String())
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
