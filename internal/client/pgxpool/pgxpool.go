package pgxpool

import (
	"context"
	"fmt"
	"time"

	"shopnexus-remastered/internal/logger"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	Url             string        `yaml:"url"`
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	Username        string        `yaml:"username"`
	Password        string        `yaml:"password"`
	Database        string        `yaml:"database"`
	MaxConnections  int32         `yaml:"maxConnections"`
	MaxConnIdleTime time.Duration `yaml:"maxConnIdleTime"`
}

func New(opts Options) (*pgxpool.Pool, error) {
	connStr := GetConnStr(opts)

	connConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, err
	}

	// Set maximum number of connections
	connConfig.MaxConns = opts.MaxConnections
	connConfig.MaxConnIdleTime = opts.MaxConnIdleTime
	connConfig.ConnConfig.OnNotice = func(conn *pgconn.PgConn, notice *pgconn.Notice) {
		logger.Log.Warn("PostgreSQL notice: " + notice.Message)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), connConfig)
	if err != nil {
		return nil, err
	}

	// Collect the custom data types once, store them in memory, and register them for every future connection.
	customTypes, err := getCustomDataTypes(context.Background(), pool)
	if err != nil {
		return nil, err
	}
	connConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		for _, t := range customTypes {
			conn.TypeMap().RegisterType(t)
		}
		return nil
	}

	// Immediately close the old pool and open a new one with the new config.
	pool.Close()
	return pgxpool.NewWithConfig(context.Background(), connConfig)
}

// Any custom DB types made with CREATE TYPE need to be registered with pgx.
// https://github.com/kyleconroy/sqlc/issues/2116
// https://stackoverflow.com/questions/75658429/need-to-update-psql-row-of-a-composite-type-in-golang-with-jack-pgx
// https://pkg.go.dev/github.com/jackc/pgx/v5/pgtype
func getCustomDataTypes(ctx context.Context, pool *pgxpool.Pool) ([]*pgtype.Type, error) {
	// Get a single connection just to load type information.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	// TODO: Currently hard code, should find a way to auto load all custom types instead
	dataTypeNames := []string{
		`"account"."type"`,
		`"account"."_type"`,
		`"account"."status"`,
		`"account"."_status"`,
		`"account"."gender"`,
		`"account"."_gender"`,
		`"account"."address_type"`,
		`"account"."_address_type"`,

		`"catalog"."comment_ref_type"`,
		`"catalog"."_comment_ref_type"`,

		`"inventory"."stock_ref_type"`,
		`"inventory"."_stock_ref_type"`,
		`"inventory"."product_status"`,
		`"inventory"."_product_status"`,

		`"order"."refund_method"`,
		`"order"."_refund_method"`,
		`"order"."invoice_type"`,
		`"order"."_invoice_type"`,
		`"order"."invoice_ref_type"`,
		`"order"."_invoice_ref_type"`,

		`"promotion"."type"`,
		`"promotion"."_type"`,
		`"promotion"."ref_type"`,
		`"promotion"."_ref_type"`,

		`"common"."resource_ref_type"`,
		`"common"."_resource_ref_type"`,
		`"common"."status"`,
		`"common"."_status"`,

		//`"system"."event_type"`,
		//`"system"."_event_type"`,
	}

	var typesToRegister []*pgtype.Type
	for _, typeName := range dataTypeNames {
		dataType, err := conn.Conn().LoadType(ctx, typeName)
		if err != nil {
			return nil, err
		}
		// You need to register only for this connection too, otherwise the array type will look for the register element type.
		conn.Conn().TypeMap().RegisterType(dataType)
		typesToRegister = append(typesToRegister, dataType)
	}
	return typesToRegister, nil
}

func GetConnStr(opts Options) string {
	if opts.Url == "" {
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
			opts.Host,
			opts.Port,
			opts.Username,
			opts.Password,
			opts.Database,
		)
	}

	return opts.Url
}
