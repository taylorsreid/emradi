package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func useNumericId(collection *core.Collection) error {
	err := collection.Fields.AddMarshaledJSONAt(0, []byte(`{
			"autogeneratePattern": "[0-999999]",
			"hidden": false,
			"max": 15,
			"min": 1,
			"name": "id",
			"pattern": "^[a-z0-9]+$",
			"presentable": false,
			"primaryKey": true,
			"required": true,
			"system": true,
			"type": "text"
		}`))
	return err
}

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
		err := useNumericId(radios)
		if err != nil {
			return err
		}
		err = app.Save(radios)
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
		err = useNumericId(roles)
		if err != nil {
			return err
		}
		err = app.Save(roles)
		if err != nil {
			return err
		}

		record := core.NewRecord(roles)
		record.Set("name", "Dispatcher")
		err = app.Save(record)
		if err != nil {
			return err
		}

		// user statuses
		userStatuses := core.NewBaseCollection("userStatuses")
		userStatuses.Fields.Add(
			&core.TextField{
				Name:     "name",
				Required: true,
			},
		)
		userStatuses.AddIndex("userStatusesNameUnique", true, "name", "")
		err = useNumericId(userStatuses)
		if err != nil {
			return err
		}
		err = app.Save(userStatuses)
		if err != nil {
			return err
		}

		record = core.NewRecord(userStatuses)
		record.Set("name", "available")
		err = app.Save(record)
		if err != nil {
			return err
		}
		record = core.NewRecord(userStatuses)
		record.Set("name", "offline")
		err = app.Save(record)
		if err != nil {
			return err
		}

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
			&core.RelationField{
				Name:         "status",
				CollectionId: userStatuses.Id,
				MinSelect:    1,
				MaxSelect:    1,
				Required:     true,
			},
			&core.NumberField{
				Name:     "meshAddress",
				Required: false,
				OnlyInt:  true,
			},
		)
		users.AddIndex("usersmeshAddressUnique", true, "meshAddress", "meshAddress != 0")
		err = useNumericId(users)
		if err != nil {
			return err
		}
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
		err = useNumericId(incidentTypes)
		if err != nil {
			return err
		}
		err = app.Save(incidentTypes)
		if err != nil {
			return err
		}

		// incidentEvents
		incidentEvents := core.NewBaseCollection("incidentEvents")
		incidentEvents.Fields.Add(
			&core.TextField{
				Name:     "title",
				Required: true,
			},
			&core.TextField{
				Name:     "details",
				Required: false,
			},
			&core.RelationField{
				Name:         "createdBy",
				CollectionId: users.Id,
				MinSelect:    1,
				MaxSelect:    1,
				Required:     true,
			},
			&core.DateField{
				Name:     "sentAt",
				Required: true,
			},
			&core.AutodateField{
				Name:     "createdAt",
				OnCreate: true,
			},
			&core.RelationField{
				Name:         "affectedUser",
				CollectionId: users.Id,
				MinSelect:    1,
				MaxSelect:    1,
			},
		)
		err = useNumericId(incidentEvents)
		if err != nil {
			return err
		}
		err = app.Save(incidentEvents)
		if err != nil {
			return err
		}

		// suggested incident events
		suggestedIncidentEvents := core.NewBaseCollection("suggestedIncidentEvents")
		suggestedIncidentEvents.Fields.Add(
			&core.TextField{
				Name:     "title",
				Required: true,
			},
		)
		suggestedIncidentEvents.AddIndex("suggestedIncidentEventsTitleUnique", true, "title", "")
		err = useNumericId(suggestedIncidentEvents)
		if err != nil {
			return err
		}
		err = app.Save(suggestedIncidentEvents)
		if err != nil {
			return err
		}

		// insert a few common example suggested incident events
		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("title", "dispatched")
		err = app.Save(record)
		if err != nil {
			return err
		}

		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("title", "acknowledged")
		err = app.Save(record)
		if err != nil {
			return err
		}

		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("title", "arrived")
		err = app.Save(record)
		if err != nil {
			return err
		}

		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("title", "information")
		app.Save(record)

		record = core.NewRecord(suggestedIncidentEvents)
		record.Set("description", "cleared")
		err = app.Save(record)
		if err != nil {
			return err
		}

		// incidents
		incidents := core.NewBaseCollection("incidents")
		incidents.Fields.Add(
			&core.RelationField{
				Name:         "incidentType",
				CollectionId: incidentTypes.Id,
				MinSelect:    1,
				MaxSelect:    1,
				Required:     true,
			},
			&core.GeoPointField{
				Name:     "coordinates",
				Required: false,
			},
			&core.TextField{
				Name:     "address",
				Required: false,
			},
			&core.RelationField{
				Name:         "createdBy",
				CollectionId: users.Id,
				MinSelect:    1,
				MaxSelect:    1,
				Required:     true,
			},
			&core.DateField{
				Name:     "sentAt",
				Required: false,
			},
			&core.AutodateField{
				Name:     "createdAt",
				OnCreate: true,
			},
			&core.DateField{
				Name:     "closedAt",
				Min:      incidents.Created,
				Required: false,
			},
			&core.RelationField{
				Name:         "events",
				CollectionId: incidentEvents.Id,
				MinSelect:    0,
				MaxSelect:    999_999,
			},
		)
		err = useNumericId(incidents)
		if err != nil {
			return err
		}
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
