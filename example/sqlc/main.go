package main

// import (
// 	"context"
// 	"fmt"
// 	"shopnexus-server/config"
// 	"shopnexus-server/internal/infras/pg"
// 	catalogdb "shopnexus-server/internal/module/catalog/db"
// 	"shopnexus-server/internal/shared/pgsqlc"
// 	"time"
// )

// func main() {
// 	cfg := config.GetConfig()
// 	pool, err := pg.New(pg.Options{
// 		Url:             cfg.Postgres.Url,
// 		Host:            cfg.Postgres.Host,
// 		Port:            cfg.Postgres.Port,
// 		Username:        cfg.Postgres.Username,
// 		Password:        cfg.Postgres.Password,
// 		Database:        cfg.Postgres.Database,
// 		MaxConnections:  cfg.Postgres.MaxConnections,
// 		MaxConnIdleTime: cfg.Postgres.MaxConnIdleTime * time.Second,
// 	})
// 	if err != nil {
// 		panic(err)
// 	}

// 	ctx := context.Background()
// 	storage := pgsqlc.NewStorage[*catalogdb.Queries](pool, catalogdb.New(pool))
// 	res, err := storage.Querier().CreateCopyDefaultTest(ctx, []catalogdb.CreateCopyDefaultTestParams{{
// 		// NullUUID:      uuid.NullUUID{UUID: uuid.New(), Valid: true},
// 		Varchar: "example",
// 		// NullVarchar:   null.StringFrom("test"),
// 		Text: "test",
// 		// NullText:      null.StringFrom("nulltext"),
// 		Int32: 123,
// 		// NullInt32:     null.IntFrom(123),
// 		Int64: 1234,
// 		// NullInt64:     null.IntFrom(1234),
// 		Float: 1.3,
// 		// NullFloat:     null.FloatFrom(1.34),
// 		Timestamp: time.Now(),
// 		// NullTimestamp: null.TimeFrom(time.Now()),
// 		Time: time.Now(),
// 		// NullTime:      null.TimeFrom(time.Now()),
// 		Json: []byte("{}"),
// 		// NullJson:      []byte("[]"),
// 	}})
// 	if err != nil {
// 		panic(err)
// 	}

// 	fmt.Println(res)

// }
