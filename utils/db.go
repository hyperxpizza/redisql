package utils

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq" //postgresql driver necessary to establish connection
)

var ctx = context.Background()

// openRedis opens a redis connection with a desired address and password
func openRedis(redisAddress, redisPassword string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddress,
		Password: redisPassword,
		DB:       0,
	})
	return rdb
}

// openSQL opens a MySQL connection with a desired user, password, and database name
func openSQL(user, password, database string) (*sql.DB, error) {
	switch password {
	case " ":
		db, err := sql.Open("mysql", fmt.Sprintf("%s@/%s", user, database))
		if err != nil {
			return nil, err
		}
		return db, nil
	default:
		db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@/%s", user, password, database))
		if err != nil {
			return nil, err
		}
		return db, nil
	}
}

// openPostgres opens a PostgreSQL connection with a desired user, password database name, host and port
func openPostgres(user, password, database, host, port string) (*sql.DB, error) {
	connectionString := fmt.Sprintf("postgres://%v:%v@%v:%v/%v?sslmode=disable", user, password, host, port, database)
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		return nil, err
	}

	//ping to check the connection
	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, err
}

// Convert is an internal function for Copy methods
func Convert(redisType, sqluser, sqlpassword, sqldatabase, sqlhost, sqlport, sqltable, redisaddr, redispass, sqlType string, log bool) error {
	var db *sql.DB
	var err error

	switch sqlType {
	case "mysql":
		db, err = openSQL(sqluser, sqlpassword, sqldatabase)
		if err != nil {
			return err
		}
	case "postgres":
		db, err = openPostgres(sqluser, sqlpassword, sqldatabase, sqlhost, sqlport)
		if err != nil {
			return err
		}
	default:
		return errors.New("Sql database type not known!")
	}

	rdb := openRedis(redisaddr, redispass)

	defer db.Close()
	defer rdb.Close()

	rows, err := db.Query(fmt.Sprintf(`SELECT * FROM %s`, sqltable))
	if err != nil {
		return err
	}

	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	if log {
		fmt.Printf("\nRedis Keys: \n")
	}
	index := 0
	switch redisType {
	case "string":
		for rows.Next() {
			if err = rows.Scan(scanArgs...); err != nil {
				return err
			}
			for i, col := range values {
				id := fmt.Sprintf("%s:%d:%s", sqltable, index, columns[i])
				err := rdb.Set(ctx, id, string(col), 0).Err()
				if err != nil {
					return err
				}
				if log {
					printKey(id, string(col))
				}
			}
			index += 1
		}
	case "list":
		for rows.Next() {
			if err = rows.Scan(scanArgs...); err != nil {
				return err
			}
			fields := []string{}
			for _, col := range values {
				fields = append(fields, string(col))
			}
			id := fmt.Sprintf("%s:%d", sqltable, index)
			err := rdb.RPush(ctx, id, fields).Err()
			if err != nil {
				return err
			}
			if log {
				printKey(id, fields)
			}
			index += 1
		}
	case "hash":
		for rows.Next() {
			if err = rows.Scan(scanArgs...); err != nil {
				return err
			}
			rowMap := make(map[string]string)
			for i, col := range values {
				rowMap[columns[i]] = string(col)
			}
			id := fmt.Sprintf("%s:%d", sqltable, index)
			err := rdb.HSet(ctx, id, rowMap).Err()
			if err != nil {
				return err
			}
			if log {
				printKey(id, rowMap)
			}
			index += 1
		}
		if err = rows.Err(); err != nil {
			return err
		}
	}
	fmt.Println("\nCopying Complete!")
	return nil
}
