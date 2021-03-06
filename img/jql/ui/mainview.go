package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jroimartin/gocui"
	"github.com/ulmenhaus/env/img/jql/osm"
	"github.com/ulmenhaus/env/img/jql/storage"
	"github.com/ulmenhaus/env/img/jql/types"
)

// MainViewMode is the current mode of the MainView.
// It determines which subview processes inputs.
type MainViewMode int

const (
	// MainViewModeTable is the mode for standard table
	// navigation, filtering, ordering, &c
	MainViewModeTable MainViewMode = iota
	// MainViewModePrompt is for when the user is being
	// prompted to enter information
	MainViewModePrompt
	// MainViewModeAlert is for when the user is being
	// shown an alert in the prompt window
	MainViewModeAlert
	// MainViewModeEdit is for when the user is editing
	// the value of a single cell
	MainViewModeEdit
)

// A MainView is the overall view of the table including headers,
// prompts, &c. It will also be responsible for managing differnt
// interaction modes if jql supports those.
type MainView struct {
	path string

	OSM     *osm.ObjectStoreMapper
	DB      *types.Database
	Table   *types.Table
	Params  types.QueryParams
	columns []string
	// TODO map[string]types.Entry and []types.Entry could both
	// be higher-level types (e.g. VerboseRow and Row)
	entries [][]types.Entry

	TableView *TableView
	Mode      MainViewMode

	switching  bool // on when transitioning modes has not yet been acknowleged by Layout
	alert      string
	promptText string
}

// NewMainView returns a MainView initialized with a given Table
func NewMainView(path, start string) (*MainView, error) {
	var store storage.Store
	if strings.HasSuffix(path, ".json") {
		store = &storage.JSONStore{}
	} else {
		return nil, fmt.Errorf("unknown file type")
	}
	mapper, err := osm.NewObjectStoreMapper(store)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	db, err := mapper.Load(f)
	if err != nil {
		return nil, err
	}
	mv := &MainView{
		path: path,
		OSM:  mapper,
		DB:   db,
	}
	return mv, mv.loadTable(start)
}

// loadTable display's the named table in the main table view
func (mv *MainView) loadTable(t string) error {
	table, ok := mv.DB.Tables[t]
	if !ok {
		return fmt.Errorf("unknown table: %s", t)
	}
	mv.Table = table
	columns := []string{}
	widths := []int{}
	for _, column := range table.Columns {
		if strings.HasPrefix(column, "_") {
			// TODO note these to skip the values as well
			continue
		}
		widths = append(widths, 20)
		columns = append(columns, column)
	}
	mv.TableView = &TableView{
		Values: [][]string{},
		Widths: widths,
	}
	mv.columns = columns
	return mv.updateTableViewContents()
}

// Layout returns the gocui object
func (mv *MainView) Layout(g *gocui.Gui) error {
	switching := mv.switching
	mv.switching = false

	// TODO hide prompt if not in prompt mode or alert mode
	maxX, maxY := g.Size()
	v, err := g.SetView("table", 0, 0, maxX-2, maxY-3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Editable = true
		v.Editor = mv
	}
	v.Clear()
	if err := mv.TableView.WriteContents(v); err != nil {
		return err
	}
	prompt, err := g.SetView("prompt", 0, maxY-3, maxX-2, maxY-1)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		prompt.Editable = true
		prompt.Editor = &PromptHandler{
			Callback: mv.promptExit,
		}
	}
	if switching {
		prompt.Clear()
	}
	switch mv.Mode {
	case MainViewModeTable:
		if _, err := g.SetCurrentView("table"); err != nil {
			return err
		}
		g.Cursor = false
	case MainViewModeAlert:
		if _, err := g.SetCurrentView("table"); err != nil {
			return err
		}
		g.Cursor = false
		fmt.Fprintf(prompt, mv.alert)
	case MainViewModePrompt:
		if _, err := g.SetCurrentView("prompt"); err != nil {
			return err
		}
		g.Cursor = true
		prompt.Write([]byte(mv.promptText))
		prompt.MoveCursor(len(mv.promptText), 0, true)
		mv.promptText = ""
	case MainViewModeEdit:
		if _, err := g.SetCurrentView("prompt"); err != nil {
			return err
		}
		g.Cursor = true
		prompt.Write([]byte(mv.promptText))
		prompt.MoveCursor(len(mv.promptText), 0, true)
		mv.promptText = ""
	}
	return nil
}

// newEntry prompts the user for the pk to a new entry and
// attempts to add an entry with that key
// TODO should just create an entry if using uuids
func (mv *MainView) newEntry() {
	mv.promptText = "create-new-entry "
	mv.switchMode(MainViewModePrompt)
}

// switchMode sets the main view's mode to the new mode and sets
// the switching flag so that Layout is aware of the transition
func (mv *MainView) switchMode(new MainViewMode) {
	mv.switching = true
	mv.Mode = new
}

// saveContents asks the osm to save the current contents to disk
func (mv *MainView) saveContents() error {
	f, err := os.OpenFile(mv.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	err = mv.OSM.Dump(mv.DB, f)
	if err != nil {
		return err
	}
	return fmt.Errorf("Wrote %s", mv.path)
}

// Edit handles keyboard inputs while in table mode
func (mv *MainView) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	if mv.Mode == MainViewModeAlert {
		mv.switchMode(MainViewModeTable)
	}

	var err error
	defer func() {
		if err != nil {
			mv.alert = err.Error()
			mv.switchMode(MainViewModeAlert)
		}
	}()

	switch key {
	case gocui.KeyArrowRight:
		mv.TableView.Move(DirectionRight)
	case gocui.KeyArrowUp:
		mv.TableView.Move(DirectionUp)
	case gocui.KeyArrowLeft:
		mv.TableView.Move(DirectionLeft)
	case gocui.KeyArrowDown:
		mv.TableView.Move(DirectionDown)
	case gocui.KeyEnter:
		mv.switchMode(MainViewModeEdit)
		row, column := mv.TableView.GetSelected()
		mv.promptText = mv.TableView.Values[row][column]
	}

	primary := mv.Table.Primary()

	switch ch {
	case 'b':
		row, column := mv.TableView.GetSelected()
		_, err = exec.Command("open", mv.TableView.Values[row][column]).CombinedOutput()
	case ':':
		mv.switchMode(MainViewModePrompt)
	case 'o':
		_, col := mv.TableView.GetSelected()
		mv.Params.OrderBy = mv.columns[col]
		mv.Params.Dec = false
		err = mv.updateTableViewContents()
	case 'O':
		_, col := mv.TableView.GetSelected()
		mv.Params.OrderBy = mv.columns[col]
		mv.Params.Dec = true
		err = mv.updateTableViewContents()
	case 'i':
		row, col := mv.TableView.GetSelected()
		key := mv.entries[row][primary].Format("")
		// TODO should use an Update so table can modify any necessary internals
		new, err := mv.Table.Entries[key][col].Add(1)
		if err != nil {
			return
		}
		mv.Table.Entries[key][col] = new
		err = mv.updateTableViewContents()
	case 'I':
		row, col := mv.TableView.GetSelected()
		key := mv.entries[row][primary].Format("")
		// TODO should use an Update so table can modify any necessary internals
		new, err := mv.Table.Entries[key][col].Add(-1)
		if err != nil {
			return
		}
		mv.Table.Entries[key][col] = new
		err = mv.updateTableViewContents()
	case 's':
		err = mv.saveContents()
	case 'n':
		mv.newEntry()
	}
}

func (mv *MainView) updateTableViewContents() error {
	mv.TableView.Values = [][]string{}
	// NOTE putting this here to support swapping columns later
	header := []string{}
	for _, col := range mv.columns {
		if mv.Params.OrderBy == col {
			if mv.Params.Dec {
				col += " ^"
			} else {
				col += " v"
			}
		}
		header = append(header, col)
	}
	mv.TableView.Header = header

	entries, err := mv.Table.Query(mv.Params)
	if err != nil {
		return err
	}
	mv.entries = entries
	for _, row := range mv.entries {
		// TODO ignore hidden columns
		formatted := []string{}
		for _, entry := range row {
			// TODO extract actual formatting
			formatted = append(formatted, entry.Format(""))
		}
		mv.TableView.Values = append(mv.TableView.Values, formatted)
	}
	return nil
}

func (mv *MainView) promptExit(contents string, finish bool, err error) {
	current := mv.Mode
	if !finish {
		return
	}
	defer func() {
		if err != nil {
			mv.switchMode(MainViewModeAlert)
			mv.alert = err.Error()
		} else {
			mv.switchMode(MainViewModeTable)
		}
	}()
	if err != nil {
		return
	}
	switch current {
	case MainViewModeEdit:
		row, column := mv.TableView.GetSelected()
		primary := mv.Table.Primary()
		key := mv.entries[row][primary].Format("")
		err = mv.Table.Update(key, mv.Table.Columns[column], contents)
		if err != nil {
			return
		}
		err = mv.updateTableViewContents()
		return
	case MainViewModePrompt:
		parts := strings.Split(contents, " ")
		if len(parts) == 0 {
			return
		}
		command := parts[0]
		switch command {
		case "create-new-entry":
			if len(parts) != 2 {
				err = fmt.Errorf("create-new-entry takes 1 arg")
				return
			}
			newPK := parts[1]
			err = mv.Table.Insert(newPK)
			if err != nil {
				return
			}
			err = mv.updateTableViewContents()
			return
		default:
			err = fmt.Errorf("unknown command: %s", contents)
		}
	}
}
