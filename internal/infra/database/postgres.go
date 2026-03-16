package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// Postgres 封装数据库连接
type Postgres struct {
	DB *sql.DB
}

// NewPostgres 创建数据库连接
func NewPostgres(databaseURL string) (*Postgres, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.PingContext(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("[Infra] Database connected")
	return &Postgres{DB: db}, nil
}

// Close 关闭连接池
func (p *Postgres) Close() error {
	log.Println("[Infra] Database closing")
	return p.DB.Close()
}
