package cypress

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

type member struct {
	ID        int32  `col:"id"`
	Name      string `col:"name"`
	YearBirth int32  `col:"year_birth"`
}

func TestDbUsage(t *testing.T) {
	testDbFile, err := ioutil.TempFile(os.TempDir(), "cytest*.db")
	if err != nil {
		t.Error("failed to create test db file", err)
		return
	}

	defer os.Remove(testDbFile.Name())

	db, err := sql.Open("sqlite3", testDbFile.Name())
	if err != nil {
		t.Error("failed to open the database file", err)
		return
	}

	defer db.Close()

	_, err = db.Exec("create table member(id INTEGER PRIMARY KEY AUTOINCREMENT, name varchar(100), year_birth int)")
	if err != nil {
		t.Error("failed to create test table", err)
		return
	}

	result, err := db.Exec("insert into member(name, year_birth) values(?, ?)", "Andy's", 1990)
	if err != nil {
		t.Error("failed to insert the test data", err)
		return
	}

	lastID, err := result.LastInsertId()
	if err != nil {
		t.Error("failed to get the last insert id", err)
		return
	}

	if lastID != 1 {
		t.Error("last insert id is not as expected, which should be 1")
		return
	}

	type log struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
	}

	l := log{}
	ctx := context.Background()
	writer := NewBufferWriter()
	SetupLogger(LogLevelDebug, writer)
	mapper := RowMapperFunc(func(row DataRow) (interface{}, error) {
		m := &member{}
		err = row.Scan(&m.ID, &m.Name, &m.YearBirth)
		return m, err
	})

	obj, err := QueryOne(ctx, db, mapper, "select id, name, year_birth from member where id=?", lastID)
	if err != nil {
		t.Error("failed to query data back", err)
		return
	}
	m := obj.(*member)
	if m.ID != 1 || m.Name != "Andy's" || m.YearBirth != 1990 {
		t.Error("data inconsistency")
		return
	}

	// check logs
	err = json.Unmarshal(writer.Buffer[0], &l)
	if err != nil {
		t.Error("bad log format", err)
		return
	}

	if l.Level != "info" || l.Message != "queryOne" {
		t.Error("unexpected log item by QueryOne")
		return
	}

	objs, err := QueryAll(ctx, db, mapper, "select id, name, year_birth from member order by id asc")
	if err != nil {
		t.Error("QueryAll failed", err)
		return
	}

	if len(objs) != 1 {
		t.Error("only one result is expected")
		return
	}

	m = objs[0].(*member)
	if m.ID != 1 || m.Name != "Andy's" || m.YearBirth != 1990 {
		t.Error("data inconsistency")
		return
	}

	// check logs
	err = json.Unmarshal(writer.Buffer[1], &l)
	if err != nil {
		t.Error("bad log format", err)
		return
	}

	if l.Level != "info" || l.Message != "queryAll" {
		t.Error("unexpected log item by QueryOne")
		return
	}

	// test smart mapper with additional column
	objs, err = QueryAll(ctx, db, NewSmartMapper(&member{}), "select id, name, year_birth, 'test' as bad_column from member order by id asc")
	if err != nil {
		t.Error(err)
		return
	}

	if len(objs) != 1 {
		t.Error("only one result is expected")
		return
	}

	m = objs[0].(*member)
	if m.ID != 1 || m.Name != "Andy's" || m.YearBirth != 1990 {
		t.Error("data inconsistency")
		return
	}

	// try query with transaction
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

	if len(objs) != 1 {
		t.Error("only one result is expected")
		return
	}

	m = objs[0].(*member)
	if m.ID != 1 || m.Name != "Andy's" || m.YearBirth != 1990 {
		t.Error("data inconsistency")
		return
	}

	obj, err = QueryOne(ctx, tx, NewSmartMapper(0), "select max(id) from member")
	if err != nil {
		t.Error(err)
		return
	}

	if 1 != obj.(int) {
		t.Error(1, obj, "are not matched")
		return
	}

	objs, err = QueryAll(ctx, tx, NewSmartMapper(""), "select name from member")
	if err != nil {
		t.Error(err)
		return
	}

	if len(objs) != 1 {
		t.Error("one item expected but got", len(objs))
		return
	}

	if objs[0].(string) != "Andy's" {
		t.Error("string of Andy's is expected, but got", objs[0].(string))
		return
	}
}
