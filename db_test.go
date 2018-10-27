package cypress

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

type member struct {
	ID        int32  `col:"id"`
	Name      string `col:"name"`
	YearBirth int32  `col:"year_birth"`
}

func TestDbUsage(t *testing.T) {
	db, err := sql.Open("mysql", "root:User_123@tcp(localhost:3306)/test")
	if err != nil {
		t.Error(err)
		return
	}

	defer db.Close()

	result, err := db.Exec("insert into member(name, year_birth) values(?, ?)", "Andy's", 1990)
	if err != nil {
		t.Error(err)
		return
	}

	lastID, err := result.LastInsertId()
	if err != nil {
		t.Error(err)
		return
	}

	fmt.Println(lastID)

	ctx := context.Background()
	SetupLogger(LogLevelDebug, os.Stdout)
	mapper := RowMapperFunc(func(row DataRow) (interface{}, error) {
		m := &member{}
		err = row.Scan(&m.ID, &m.Name, &m.YearBirth)
		return m, err
	})

	obj, err := QueryOne(ctx, db, mapper, "select id, name, year_birth from member where id=?", lastID)
	if err != nil {
		t.Error(err)
		return
	}

	m := obj.(*member)
	fmt.Println(m.ID, m.Name, m.YearBirth)

	objs, err := QueryAll(ctx, db, mapper, "select id, name, year_birth from member order by id asc")
	if err != nil {
		t.Error(err)
		return
	}

	for _, obj = range objs {
		m = obj.(*member)
		fmt.Println(m.ID, m.Name, m.YearBirth)
	}

	objs, err = QueryAll(ctx, db, NewSmartMapper(&member{}), "select id, name, year_birth from member order by id asc")
	if err != nil {
		t.Error(err)
		return
	}

	for _, obj = range objs {
		m = obj.(*member)
		fmt.Println(m.ID, m.Name, m.YearBirth)
	}

	objs, err = QueryAll(ctx, db, NewSmartMapper(&member{}), "select id, name, year_birth from member order by id asc")
	if err != nil {
		t.Error(err)
		return
	}

	for _, obj = range objs {
		m = obj.(*member)
		fmt.Println(m.ID, m.Name, m.YearBirth)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: false})
	if err != nil {
		t.Error(err)
		return
	}

	defer tx.Rollback()
	objs, err = QueryAll(ctx, tx, mapper, "select id, name, year_birth from member order by id asc")
	if err != nil {
		t.Error(err)
		return
	}

	for _, obj = range objs {
		m = obj.(*member)
		fmt.Println(m.ID, m.Name, m.YearBirth)
	}

	obj, err = QueryOne(ctx, tx, NewSmartMapper(0), "select max(id) from member")
	if err != nil {
		t.Error(err)
		return
	}

	if int(lastID) != obj.(int) {
		t.Error(lastID, obj, "are not matched")
		return
	}

	objs, err = QueryAll(ctx, tx, NewSmartMapper(""), "select name from member")
	if err != nil {
		t.Error(err)
		return
	}

	for _, obj = range objs {
		s := obj.(string)
		fmt.Println(s)
	}
}
