package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/mjl-/duit"
	"github.com/mjl-/filterlist"
)

type connUI struct {
	config      connectionConfig
	db          *sql.DB
	databaseBox *duit.Box // for the selected database, after connecting

	cancelConnectFunc context.CancelFunc

	unconnected duit.UI
	connect     *duit.Button
	status      *duit.Label
	split       *duit.Split
	databases   *filterlist.Filterlist

	duit.Box
}

func (ui *connUI) layout() {
	dui.MarkLayout(ui)
}

func (ui *connUI) error(msg string) {
	ui.status.Text = msg
	ui.Box.Kids = duit.NewKids(ui.unconnected)
	dui.MarkLayout(nil)
}

func newConnUI(config connectionConfig) (ui *connUI) {
	noDBUI := middle(label("select a database on the left"))

	var connecting duit.UI

	cancel := &duit.Button{
		Text: "cancel",
		Click: func() (e duit.Event) {
			if ui.cancelConnectFunc != nil {
				ui.cancelConnectFunc()
				ui.cancelConnectFunc = nil
			} else {
				// xxx it seems lib/pq doesn't cancel queries when it's causing a connect to an (unreachable) server
				log.Printf("already canceled...\n")
			}
			return
		},
	}

	ui = &connUI{config: config}
	ui.connect = &duit.Button{
		Text:     "connect",
		Colorset: &dui.Primary,
		Click: func() (e duit.Event) {
			defer ui.layout()
			ui.Box.Kids = duit.NewKids(connecting)
			ui.status.Text = ""

			db, err := sql.Open(ui.config.Type, ui.config.connectionString(ui.config.Database))
			if err != nil {
				ui.error(fmt.Sprintf("error: %s", err))
				return
			}

			ctx, cancelFunc := context.WithTimeout(context.Background(), 15*time.Second)
			ui.cancelConnectFunc = cancelFunc

			go func() {
				lcheck, handle := errorHandler(func(err error) {
					if db != nil {
						db.Close()
					}
					dui.Call <- func() {
						ui.error(fmt.Sprintf("error: %s", err))
					}
				})
				defer handle()

				var q string
				switch ui.config.Type {
				default:
					panic("bad connection type")
				case "", "postgres":
					q = `select datname from pg_database where not datistemplate order by datname asc`
				case "mysql":
					q = `select schema_name from information_schema.schemata order by schema_name in ('information_schema', 'performance_schema', 'sys', 'mysql') asc, schema_name asc`
				case "sqlserver":
					q = `select name from master.dbo.sysdatabases where name not in ('master', 'tempdb', 'model', 'msdb') order by name asc`
				}

				var dbNames []string
				rows, err := db.QueryContext(ctx, q)
				lcheck(err, "listing databases")
				defer rows.Close()
				for rows.Next() {
					var name string
					err = rows.Scan(&name)
					lcheck(err, "scanning row")
					dbNames = append(dbNames, name)
				}
				lcheck(rows.Err(), "reading row")

				dbValues := make([]*duit.ListValue, len(dbNames))
				var sel *duit.ListValue
				for i, name := range dbNames {
					lv := &duit.ListValue{
						Text:     name,
						Value:    newDBUI(ui, name),
						Selected: name == ui.config.Database,
					}
					dbValues[i] = lv
					if lv.Selected {
						sel = lv
						sel.Value.(*dbUI).init()
					}
				}

				dui.Call <- func() {
					ui.cancelConnectFunc = nil

					defer ui.layout()
					ui.db = db
					topUI.disconnect.Disabled = false
					ui.databases.Values = dbValues
					ui.databases.Filter()
					nui := noDBUI
					if sel != nil {
						nui = sel.Value.(*dbUI)
					}
					ui.Box.Kids = duit.NewKids(ui.split)
					ui.Box.Kids[0].ID = "databases"
					ui.databaseBox.Kids = duit.NewKids(nui)
				}
			}()
			return
		},
	}
	edit := &duit.Button{
		Text: "edit",
		Click: func() (e duit.Event) {
			sUI := newSettingsUI(ui.config, false, func() {
				ui.Box.Kids = duit.NewKids(ui.unconnected)
				ui.layout()
				dui.Focus(ui.connect)
			})
			ui.Box.Kids = duit.NewKids(sUI)
			ui.layout()
			dui.Focus(sUI.name)
			return
		},
	}
	ui.status = &duit.Label{}
	ui.unconnected = middle(ui.status, ui.connect, edit)
	connecting = middle(label("connecting..."), cancel)
	ui.databases = filterlist.NewFilterlist(dui, &duit.List{Values: nil})
	ui.databases.List.Changed = func(index int) (e duit.Event) {
		lv := ui.databases.List.Values[index]
		nui := noDBUI
		shouldConnect := false
		if lv.Selected {
			dbUI := lv.Value.(*dbUI)
			nui = dbUI
			shouldConnect = dbUI.db == nil
		}
		ui.databaseBox.Kids = duit.NewKids(nui)
		dui.MarkLayout(ui)
		if shouldConnect {
			go lv.Value.(*dbUI).init()
		} else {
			dui.Focus(lv.Value.(*dbUI).tables.Search)
		}
		return
	}
	ui.databaseBox = &duit.Box{
		Kids: duit.NewKids(noDBUI),
	}
	ui.split = &duit.Split{
		Gutter:     1,
		Background: dui.Gutter,
		Split: func(width int) []int {
			return ui.splitDimensions(width)
		},
		Kids: duit.NewKids(
			&duit.Box{
				Kids: duit.NewKids(
					duit.CenterUI(duit.SpaceXY(4, 2), &duit.Label{Text: "databases", Font: bold}),
					ui.databases,
				),
			},
			ui.databaseBox,
		),
	}
	ui.Box.Kids = duit.NewKids(ui.unconnected)
	return
}

func (ui *connUI) splitDimensions(width int) []int {
	if topUI.hideLeftBars {
		return []int{0, width}
	}
	first := dui.Scale(200)
	if first > width/2 {
		first = width / 2
	}
	return []int{first, width - first}
}

func (ui *connUI) ensureLeftBars() {
	k := ui.Box.Kids[0]
	if k.UI != ui.split {
		return
	}
	width := k.R.Dx()
	ui.split.Dimensions(dui, ui.splitDimensions(width))
}

func (ui *connUI) disconnect() {
	// xxx todo: close all lower dbUI db connections
	ui.db.Close()
	ui.db = nil
	ui.Box.Kids = duit.NewKids(ui.unconnected)
	dui.MarkLayout(ui)
}
