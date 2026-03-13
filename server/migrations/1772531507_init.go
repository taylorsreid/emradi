package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {

		// radios
		radios := core.NewBaseCollection("radios")
		var minChannelIndex float64 = 0
		var maxChannelIndex float64 = 7
		radios.Fields.Add(
			&core.TextField{
				Name:     "name",
				Required: true,
			},
			&core.BoolField{
				Name: "selected",
			},
			&core.TextField{
				Name:     "path",
				Required: true,
			},
			&core.SelectField{
				Name:      "modemMode",
				Values:    []string{"lf", "ls", "vls", "ms", "mf", "sl", "sf", "lm"},
				MaxSelect: 1,
			},
			&core.NumberField{
				Name:    "channelIndex",
				Min:     &minChannelIndex,
				Max:     &maxChannelIndex,
				OnlyInt: true,
			},
		)
		radios.AddIndex("radiosNameUnique", true, "name", "")
		radios.AddIndex("radiosSelectedUnique", true, "selected", "")
		err := app.Save(radios)
		if err != nil {
			return err
		}

		// roles
		roles := core.NewBaseCollection("roles")
		roles.Fields.Add(
			&core.TextField{
				Name:     "name",
				Required: true,
			},
			&core.FileField{
				Name: "icon",
				// Thumbs: , // TODO: figure out optimal dimensions on the frontend
			},
		)
		roles.AddIndex("roleNameUnique", true, "name", "")
		err = app.Save(roles)
		if err != nil {
			return err
		}
		record := core.NewRecord(roles)
		record.Set("name", "Dispatcher")
		app.Save(record)

		// user statuses
		// userStatuses := core.NewBaseCollection("userStatuses")
		// userStatuses.Fields.Add(
		// 	&core.TextField{
		// 		Name:     "name",
		// 		Required: true,
		// 	},
		// )
		// userStatuses.AddIndex("userStatusesNameUnique", true, "name", "")
		// err = app.Save(userStatuses)
		// if err != nil {
		// 	return err
		// }
		// record = core.NewRecord(userStatuses)
		// record.Set("name", "available")
		// app.Save(record)
		// record = core.NewRecord(userStatuses)
		// record.Set("name", "offline")
		// app.Save(record)

		// users
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		users.Fields.Add(
			&core.RelationField{
				Name:          "roles",
				CollectionId:  roles.Id,
				CascadeDelete: true,
				MinSelect:     1,
				MaxSelect:     999,
				Required:      true,
			},
			&core.TextField{
				Name: "status",
			},
			// &core.SelectField{
			// 	Name:      "contactMethod",
			// 	Values:    []string{"ip", "mesh"},
			// 	MaxSelect: 1,
			// 	Required:  true,
			// },
			&core.NumberField{
				Name: "meshAddress",
			},
		)
		users.AddIndex("usersmeshAddressUnique", true, "meshAddress", "")
		err = app.Save(users)
		if err != nil {
			return err
		}

		// incident types
		incidentTypes := core.NewBaseCollection("incidentTypes")
		incidentTypes.Fields.Add(
			&core.TextField{
				Name:     "name",
				Required: true,
			},
			&core.FileField{
				Name: "icon",
				// Thumbs: , // TODO:
			},
			&core.RelationField{
				Name:          "suggestedRoles",
				CollectionId:  roles.Id,
				CascadeDelete: true,
				MinSelect:     0,
				MaxSelect:     999,
				Required:      false,
			},
		)
		incidentTypes.AddIndex("incidentTypeNameUnique", true, "name", "")
		err = app.Save(incidentTypes)
		if err != nil {
			return err
		}

		// incidentEvents
		incidentEvents := core.NewBaseCollection("incidentEvents")
		incidentEvents.Fields.Add(
			&core.TextField{
				Name:     "description",
				Required: true,
			},
			&core.TextField{
				Name: "notes",
			},
			&core.AutodateField{
				Name:     "created",
				OnCreate: true,
			},
			&core.RelationField{
				Name:         "createdBy",
				CollectionId: users.Id,
				MinSelect:    1,
				MaxSelect:    1,
				Required:     true,
			},
			&core.RelationField{
				Name:         "affectedUser",
				CollectionId: users.Id,
				MinSelect:    1,
				MaxSelect:    1,
			},
		)
		err = app.Save(incidentEvents)
		if err != nil {
			return err
		}

		// suggested incident events
		suggestedIncidentEvents := core.NewBaseCollection("suggestedIncidentEvents")
		suggestedIncidentEvents.Fields.Add(
			&core.TextField{
				Name:     "description",
				Required: true,
			},
		)
		suggestedIncidentEvents.AddIndex("suggestedIncidentEventsDescriptionUnique", true, "description", "")
		err = app.Save(suggestedIncidentEvents)
		if err != nil {
			return err
		}

		// insert a few common example suggested incident events
		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("description", "dispatched")
		err = app.Save(record)

		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("description", "acknowledged")
		app.Save(record)

		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("description", "arrived")
		app.Save(record)

		// record = core.NewRecord(suggestedIncidentEvents)
		// record.Set("description", "information")
		// app.Save(record)

		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("description", "cleared")
		app.Save(record)

		// incidents
		incidents := core.NewBaseCollection("incidents")
		incidents.Fields.Add(
			&core.GeoPointField{
				Name:     "location",
				Required: true,
			},
			&core.RelationField{
				Name:         "incidentType",
				CollectionId: incidentTypes.Id,
				MinSelect:    1,
				MaxSelect:    1,
				Required:     true,
			},
			&core.RelationField{
				Name:         "createdBy",
				CollectionId: users.Id,
				MinSelect:    1,
				MaxSelect:    1,
				Required:     true,
			},
			&core.DateField{
				Name: "closedAt",
				Min:  incidents.Created,
			},
			&core.RelationField{
				Name:         "events",
				CollectionId: incidentEvents.Id,
				MinSelect:    0,
				MaxSelect:    9_999,
			},
		)
		err = app.Save(incidents)
		if err != nil {
			return err
		}

		return nil
	}, func(app core.App) error {
		// add down queries...

		return nil
	})
}
