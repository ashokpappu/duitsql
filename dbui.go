package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mjl-/duit"
)

type dbUI struct {
	connUI *connUI
	dbName string
	db     *sql.DB

	tableList *duit.Gridlist
	contentUI *duit.Box // holds 1 kid, the editUI, tableUI, viewUI or placeholder label

	duit.Box // holds either box with status message, or box with tableList and contentUI
}

func newDBUI(cUI *connUI, dbName string) (ui *dbUI) {
	ui = &dbUI{
		connUI: cUI,
		dbName: dbName,
	}
	return
}

func (ui *dbUI) layout() {
	dui.MarkLayout(nil) // xxx
}

func (ui *dbUI) error(err error) {
	defer ui.layout()
	msg := &duit.Label{Text: "error: " + err.Error()}
	retry := &duit.Button{
		Text: "retry",
		Click: func(e *duit.Event) {
			go ui.init()
		},
	}
	ui.Box.Kids = duit.NewKids(middle(msg, retry))
}

func (ui *dbUI) init() {
	setError := func(err error) {
		dui.Call <- func() {
			ui.error(err)
		}
	}

	db, err := sql.Open("postgres", ui.connUI.cc.connectionString(ui.dbName))
	if err != nil {
		setError(err)
		return
	}

	ctx, cancelQueryFunc := context.WithTimeout(context.Background(), 15*time.Second)
	dui.Call <- func() {
		defer ui.layout()
		ui.Box.Kids = duit.NewKids(
			middle(
				&duit.Label{Text: "listing tables..."},
				&duit.Button{
					Text: "cancel",
					Click: func(e *duit.Event) {
						cancelQueryFunc()
					},
				},
			),
		)
	}
	q := `
		select coalesce(json_agg(x.*), '[]') from (
			select
				table_type = 'VIEW' as is_view,
				table_schema in ('pg_catalog', 'information_schema') as internal, table_schema || '.' || table_name as name
			from information_schema.tables
			order by internal asc, name asc
		) x
	`
	type object struct {
		IsView bool   `json:"is_view"`
		Name   string `json:"name"`
	}
	var objects []object
	err = parseRow(db.QueryRowContext(ctx, q), &objects)
	if err != nil {
		setError(fmt.Errorf("listing tables in database: %s", err))
		return
	}

	eUI := newEditUI(ui)
	values := make([]*duit.Gridrow, 1+len(objects))
	values[0] = &duit.Gridrow{
		Selected: true,
		Values:   []string{"", "<sql>"},
		Value:    eUI,
	}
	for i, obj := range objects {
		var objUI duit.UI
		var kind string
		if obj.IsView {
			objUI = newViewUI(ui, obj.Name)
			kind = "V"
		} else {
			objUI = newTableUI(ui, obj.Name)
			kind = "T"
		}
		values[i+1] = &duit.Gridrow{
			Values: []string{
				kind,
				obj.Name,
			},
			Value: objUI,
		}
	}

	dui.Call <- func() {
		defer ui.layout()
		ui.db = db
		ui.tableList = &duit.Gridlist{
			Header: duit.Gridrow{
				Values: []string{"", ""},
			},
			Rows: values,
			Changed: func(index int, r *duit.Event) {
				defer ui.layout()
				lv := ui.tableList.Rows[index]
				var selUI duit.UI
				if !lv.Selected {
					selUI = duit.NewMiddle(&duit.Label{Text: "select <sql>, or a a table or view on the left"})
				} else {
					selUI = lv.Value.(duit.UI)
					switch objUI := selUI.(type) {
					case *editUI:
					case *tableUI:
						objUI.init()
					case *viewUI:
						objUI.init()
					}
				}
				ui.contentUI.Kids = duit.NewKids(selUI)
			},
		}
		ui.contentUI = &duit.Box{
			Kids: duit.NewKids(eUI),
		}
		ui.Box.Kids = duit.NewKids(
			&duit.Horizontal{
				Split: func(width int) []int {
					first := dui.Scale(200)
					if first > width/2 {
						first = width / 2
					}
					return []int{first, width - first}
				},
				Kids: duit.NewKids(
					&duit.Box{
						Kids: duit.NewKids(
							duit.CenterUI(duit.SpaceXY(4, 2), &duit.Label{Text: "tables", Font: bold}),
							duit.NewScroll(ui.tableList),
						),
					},
					ui.contentUI,
				),
			},
		)
	}
}
