package main

import (
	"fmt"
	"context"
	"database/sql"
	"net/http"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"time"
	"github.com/jackc/pgx/v4/pgxpool"
	"strconv"
)

const (
	host     = "db"
	port     = 5432
	user     = "rinha"
	password = "rinha123"
	dbname   = "rinha"
)

func getConnection() *pgxpool.Pool {
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
    "password=%s dbname=%s sslmode=disable",
    host, port, user, password, dbname)


	config, err := pgxpool.ParseConfig(psqlInfo)
	if err != nil {
		panic(err)
	}
	config.MaxConns = 10

	pool, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		panic(err)
	}

	return pool
	
}

type TransacaoRequest struct {
    Valor 		int 		`json:"valor"`
    Tipo 		string 		`json:"tipo"`
	Descricao 	string 		`json:"descricao"`
}

type Cliente struct {
	ID          int
	Nome        string
	Saldo       int64
	Limite      int64
	DataCriacao time.Time
}

type Transacao struct {
	ID           sql.NullInt64
	ClienteID    sql.NullInt64
	Valor        sql.NullInt64
	Tipo         sql.NullString
	Descricao    sql.NullString
	RealizadaEm  sql.NullTime
}

type TransacaoResponse struct {
	Valor        	int64		`json:"valor"`
	Tipo         	string		`json:"tipo"`
	Descricao   	string		`json:"descricao"`
	RealizadaEm  	time.Time	`json:"realizada_em"`
}

func main() {
	gin.SetMode(gin.ReleaseMode)

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
		defer rows.Close()
		if err != nil {
			panic(err)
		}
		
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

		saldo := map[string]interface{}{
			"total":       clientes[0].Saldo,
			"data_extrato": time.Now().UTC(),
			"limite":      clientes[0].Limite,
		}
		
		var ultimasTransacoes []TransacaoResponse 

		for _,  transacao := range transacoes {
			if transacao.Valor.Valid {
				ultimaTransacao := TransacaoResponse{
					Valor: transacao.Valor.Int64,
					Tipo: transacao.Tipo.String,
					Descricao: transacao.Descricao.String,
					RealizadaEm: transacao.RealizadaEm.Time,
				}
				ultimasTransacoes = append(ultimasTransacoes, ultimaTransacao)
			}
			
		}


		resposta := gin.H{
			"saldo": saldo,
			"ultimas_transacoes": ultimasTransacoes,
		}
		c.JSON(http.StatusOK, resposta)
	})

	r.POST("/clientes/:id/transacoes", func(c *gin.Context) {
		clienteId := c.Param("id")
		clienteIdAsNum, err := strconv.Atoi(clienteId)
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

		defer rows.Close()
		if err != nil {
			panic(err)
		}
		
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
			"saldo": saldo,
		})

	})
	
	r.Run(":8080")
}