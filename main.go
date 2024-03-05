package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4/pgxpool"
	_ "github.com/lib/pq"
)

const (
	port     = 5432
	user     = "rinha"
	password = "rinha123"
	dbname   = "rinha"
)

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

func toInt(str string) int64 {
	num, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		panic("Erro ao converter a string para int")
	}
	return num
}

func getConnection() *pgxpool.Pool {
	var host = getEnv("DB_HOST", "localhost")
	var maxConns = getEnv("DB_MAX_CONNS", "10")
	var minConns = getEnv("DB_MIN_CONNS", "1")

	fmt.Println("DB_HOST     : 	" + host)
	fmt.Println("DB_MAX_CONNS: 	" + maxConns)
	fmt.Println("DB_MIN_CONNS:	" + minConns)

	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	config, err := pgxpool.ParseConfig(psqlInfo)
	if err != nil {
		panic(err)
	}

	config.MaxConns = int32(toInt(maxConns))
	config.MinConns = int32(toInt(minConns))

	pool, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		panic(err)
	}

	return pool

}

type TransacaoRequest struct {
	Valor     int    `json:"valor"`
	Tipo      string `json:"tipo"`
	Descricao string `json:"descricao"`
}

type Cliente struct {
	ID          int
	Nome        string
	Saldo       int64
	Limite      int64
	DataCriacao time.Time
}

type Transacao struct {
	ID          sql.NullInt64
	ClienteID   sql.NullInt64
	Valor       sql.NullInt64
	Tipo        sql.NullString
	Descricao   sql.NullString
	RealizadaEm sql.NullTime
}

type TransacaoResponse struct {
	Valor       int64     `json:"valor"`
	Tipo        string    `json:"tipo"`
	Descricao   string    `json:"descricao"`
	RealizadaEm time.Time `json:"realizada_em"`
}

type SaldoResponse struct {
	Total       int64     `json:"total"`
	DataExtrato time.Time `json:"data_extrato"`
	Limite      int64     `json:"limite"`
}

type ExtratoResponse struct {
	Saldo             SaldoResponse       `json:"saldo"`
	UltimasTransacoes []TransacaoResponse `json:"ultimas_transacoes"`
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.Discard
	db := getConnection()
	defer db.Close()
	r := gin.Default()

	r.GET("/clientes/:id/extrato", func(c *gin.Context) {
		clienteId := c.Param("id")
		clienteIdAsNum, err := strconv.Atoi(clienteId)
		if err != nil {
			c.Status(http.StatusUnprocessableEntity)
			return
		}
		if clienteIdAsNum < 0 || clienteIdAsNum > 5 {
			c.Status(http.StatusNotFound)
			return
		}

		query := `
		SELECT c.*, t.*
		FROM clientes c
		LEFT JOIN transacoes t ON c.id = t.cliente_id
		WHERE c.id = $1
		ORDER BY t.realizada_em DESC
		LIMIT 10;
	`

		rows, err := db.Query(context.Background(), query, clienteId)

		if err != nil {
			panic(err)
		}

		defer rows.Close()

		var clientes []Cliente
		var transacoes []Transacao

		for rows.Next() {
			var cliente Cliente
			var transacao Transacao

			err := rows.Scan(
				&cliente.ID, &cliente.Nome, &cliente.Limite, &cliente.Saldo,
				&transacao.ID, &transacao.ClienteID, &transacao.Valor, &transacao.Tipo, &transacao.Descricao, &transacao.RealizadaEm,
			)
			if err != nil {
				panic(err)
			}
			clientes = append(clientes, cliente)
			transacoes = append(transacoes, transacao)
		}

		var ultimasTransacoes []TransacaoResponse

		for _, transacao := range transacoes {
			if transacao.Valor.Valid {
				ultimaTransacao := TransacaoResponse{
					Valor:       transacao.Valor.Int64,
					Tipo:        transacao.Tipo.String,
					Descricao:   transacao.Descricao.String,
					RealizadaEm: transacao.RealizadaEm.Time,
				}
				ultimasTransacoes = append(ultimasTransacoes, ultimaTransacao)
			}

		}
		saldoResponse := SaldoResponse{
			Total:       clientes[0].Saldo,
			DataExtrato: time.Now().UTC(),
			Limite:      clientes[0].Limite,
		}

		resposta := ExtratoResponse{
			Saldo:             saldoResponse,
			UltimasTransacoes: ultimasTransacoes,
		}

		c.JSON(http.StatusOK, resposta)
	})

	r.POST("/clientes/:id/transacoes", func(c *gin.Context) {
		clienteId := c.Param("id")
		clienteIdAsNum, err := strconv.Atoi(clienteId)

		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		if clienteIdAsNum < 0 || clienteIdAsNum > 5 {
			c.Status(http.StatusNotFound)
			return
		}

		var transacao TransacaoRequest
		if err := c.ShouldBindJSON(&transacao); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}

		if transacao.Valor < 0 {
			c.Status(http.StatusUnprocessableEntity)
			return
		}

		if transacao.Tipo != "d" && transacao.Tipo != "c" {
			c.Status(http.StatusUnprocessableEntity)
			return
		}

		if len(transacao.Descricao) < 1 || len(transacao.Descricao) > 10 {
			c.Status(http.StatusUnprocessableEntity)
			return
		}
		var valorTransacao = transacao.Valor
		if transacao.Tipo == "d" {
			valorTransacao = -valorTransacao
		}

		rows, err := db.Query(context.Background(), `
			UPDATE clientes 
			SET saldo = saldo + $2 
			WHERE 
				id = $1 
				AND $2 + saldo + limite >= 0
			RETURNING saldo, limite
			`, clienteId, valorTransacao)

		if err != nil {
			panic(err)
		}

		defer rows.Close()

		var saldo, limite int
		for rows.Next() {
			if err := rows.Scan(&saldo, &limite); err != nil {
				panic(err)
			}
		}

		if limite == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return
		}

		sqlStatement := `
		INSERT INTO transacoes (cliente_id, valor, tipo, descricao)
		VALUES ($1, $2, $3, $4)
		`

		_, err2 := db.Exec(context.Background(), sqlStatement, clienteId, transacao.Valor, transacao.Tipo, transacao.Descricao)
		if err2 != nil {
			panic(err)
		}

		c.JSON(http.StatusOK, gin.H{
			"limite": limite,
			"saldo":  saldo,
		})

	})
	var port = ":" + getEnv("HTTP_PORT", "8080")
	fmt.Println("DB_MIN_CONNS:	" + port)
	r.Run(port)
}
