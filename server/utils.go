package main

// import (
// 	"github.com/pocketbase/pocketbase/core"
// 	"github.com/pocketbase/pocketbase/tools/filesystem"
// )

// func NewRole(name string, icon *filesystem.File) (*core.Record, error) {
// 	roles, err := app.FindCollectionByNameOrId("roles")
// 	if err != nil {
// 		return nil, err
// 	}
// 	record := core.NewRecord(roles)
// 	record.Set("name", name)
// 	if icon != nil {
// 		record.Set("icon", icon)
// 	}
// 	return record, nil
// }

// func NewUserStatus(name string) (*core.Record, error) {
// 	userStatuses, err := app.FindCollectionByNameOrId("userStatuses")
// 	if err != nil {
// 		return nil, err
// 	}
// 	record := core.NewRecord(userStatuses)
// 	record.Set("name", name)
// 	return record, nil
// }

// func NewUser() (*core.Record, error) {
// 	users, err := app.FindCollectionByNameOrId("users")
// 	if err != nil {
// 		return nil, err
// 	}
// 	record := core.NewRecord(users)
// 	record.Set()
// }
