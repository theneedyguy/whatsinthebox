package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type Database struct {
	DBFilePath string
}

// Define a custom nullable string type for JSON marshaling
type JSONNullString struct {
	sql.NullString
}

// MarshalJSON customizes JSON encoding for JSONNullString
func (ns JSONNullString) MarshalJSON() ([]byte, error) {
	if ns.Valid {
		return json.Marshal(ns.String)
	}
	return json.Marshal(nil)
}

type Box struct {
	ID        int            `json:"id"`
	Name      string         `json:"name"`
	Label     JSONNullString `json:"label"`
	CreatedAt time.Time      `json:"created_at"`
}

type BoxContent struct {
	BoxID     int
	BoxName   string
	BoxLabel  sql.NullString
	ContentID sql.NullInt64
	Name      sql.NullString
	Quantity  sql.NullInt64
	AddedAt   sql.NullTime
}

func (d *Database) Init() *sql.DB {

	db, err := sql.Open("sqlite3", d.DBFilePath)
	if err != nil {
		log.Fatal(err)
	}

	createTable := `
    CREATE TABLE IF NOT EXISTS boxes (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
        label TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TABLE IF NOT EXISTS contents (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        box_id INTEGER,
        name TEXT,
        quantity INTEGER,
        added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (box_id) REFERENCES boxes(id)
    );`

	if _, err := db.Exec(createTable); err != nil {
		log.Fatalf("Could not create table: %v", err)
	}
	return db
}

func (d *Database) GetBoxesTotal(db *sql.DB) (int, error) {
	query := `SELECT COUNT(*) AS box_count FROM boxes;`
	var boxCount int

	err := db.QueryRow(query).Scan(&boxCount)
	if err != nil {
		return 0, err
	}
	return boxCount, nil
}

func (d *Database) GetBoxesPaginated(db *sql.DB, page int, pageSize int) ([]Box, error) {
	offset := (page * pageSize) / pageSize

	query := `SELECT id, name, label, created_at FROM boxes ORDER BY created_at DESC LIMIT ? OFFSET ?`

	rows, err := db.Query(query, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var boxes []Box
	for rows.Next() {
		var box Box
		if err := rows.Scan(&box.ID, &box.Name, &box.Label, &box.CreatedAt); err != nil {
			return nil, err
		}
		boxes = append(boxes, box)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return boxes, nil
}

func (d *Database) GetBoxesByTextV0(db *sql.DB, searchText string) ([]Box, error) {
	query := `
	SELECT id, name, label, created_at
	FROM boxes 
	WHERE name LIKE '%' || ? || '%' 
	OR label LIKE '%' || ? || '%';`

	rows, err := db.Query(query, searchText, searchText)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var boxes []Box
	for rows.Next() {
		var box Box
		if err := rows.Scan(&box.ID, &box.Name, &box.Label, &box.CreatedAt); err != nil {
			return nil, err
		}
		boxes = append(boxes, box)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return boxes, nil
}

func (d *Database) GetBoxes(db *sql.DB) ([]Box, error) {
	query := `SELECT id, label, name, created_at FROM boxes`
	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var boxes []Box
	for rows.Next() {
		var box Box
		if err := rows.Scan(&box.ID, &box.Label, &box.Name, &box.CreatedAt); err != nil {
			return nil, err
		}
		if !box.Label.Valid {
			box.Label.String = "unlabeled"
		}
		boxes = append(boxes, box)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return boxes, nil
}

func (d *Database) GetBoxContent(db *sql.DB, boxID int) ([]BoxContent, error) {
	query := `
    SELECT 
        boxes.id AS box_id,
        boxes.name AS box_name, 
        boxes.label AS box_label, 
        contents.id AS content_id, 
        contents.name AS content_name, 
        contents.quantity AS content_quantity, 
        contents.added_at AS content_added_at	
    FROM 
        boxes
    LEFT JOIN 
        contents ON boxes.id = contents.box_id
    WHERE 
        boxes.id = ?
	ORDER BY contents.added_at DESC`

	rows, err := db.Query(query, boxID)
	if err != nil {
		log.Fatalln(err)
	}
	defer rows.Close()

	var boxContents []BoxContent
	for rows.Next() {
		var content BoxContent
		if err := rows.Scan(&content.BoxID, &content.BoxName, &content.BoxLabel, &content.ContentID, &content.Name, &content.Quantity, &content.AddedAt); err != nil {
			return nil, err
		}
		if !content.BoxLabel.Valid {
			content.BoxLabel.String = "unlabeled"
		}
		boxContents = append(boxContents, content)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return boxContents, nil
}

func (d *Database) UpdateBoxContent(db *sql.DB, contentID int, newName string, newQuantity int) error {
	query := `UPDATE contents SET name = ?, quantity = ? WHERE id = ?`
	result, err := db.Exec(query, newName, newQuantity, contentID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()

	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fmt.Errorf("no content found with id %d", contentID)
	}
	return nil
}

func (d *Database) CreateBox(db *sql.DB, name string, label string) error {
	query := `INSERT INTO boxes (name, label) VALUES (?, ?)`
	result, err := db.Exec(query, name, label)
	if err != nil {
		return err
	}
	boxId, err := result.LastInsertId()
	if err != nil {
		return err
	}
	log.Println(boxId)
	return nil
}

func (d *Database) DeleteBox(db *sql.DB, id int) error {
	// Start a transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// Defer a rollback in case something fails.
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// First, delete all contents associated with the box
	deleteContentsQuery := `DELETE FROM contents WHERE box_id = ?`
	_, err = tx.Exec(deleteContentsQuery, id)
	if err != nil {
		return fmt.Errorf("failed to delete contents: %w", err)
	}

	// Then, delete the box itself
	deleteBoxQuery := `DELETE FROM boxes WHERE id = ?`
	_, err = tx.Exec(deleteBoxQuery, id)
	if err != nil {
		return fmt.Errorf("failed to delete box: %w", err)
	}

	// Commit the transaction
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (d *Database) UpdateBox(db *sql.DB, id int, newName string, newLabel string) error {
	query := `UPDATE boxes SET name = ?, label = ? WHERE id = ?`
	result, err := db.Exec(query, newName, newLabel, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()

	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fmt.Errorf("no box found with id %d", id)
	}
	return nil
}

func (d *Database) CreateItem(db *sql.DB, boxId int, name string, quantity int) error {
	query := `INSERT INTO contents (name, quantity, box_id) VALUES (?, ?, ?)`
	result, err := db.Exec(query, name, quantity, boxId)
	if err != nil {
		return err
	}
	contentId, err := result.LastInsertId()
	if err != nil {
		return err
	}
	log.Println(contentId)
	return nil
}

func (d *Database) MoveItem(db *sql.DB, sourceBoxID, destBoxID, contentId int) error {
	query := `
	UPDATE contents
	SET box_id = ?
	WHERE box_id = ? AND id = ?`

	// Start a transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// Defer a rollback in case something fails.
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	_, err = tx.Exec(query, destBoxID, sourceBoxID, contentId)
	if err != nil {
		return err
	}
	// Commit the transaction
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (d *Database) DeleteItem(db *sql.DB, id int) error {
	// Start a transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// Defer a rollback in case something fails.
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Delete item from box
	deleteContentsQuery := `DELETE FROM contents WHERE id = ?`
	_, err = tx.Exec(deleteContentsQuery, id)
	if err != nil {
		return fmt.Errorf("failed to delete contents from box: %w", err)
	}

	// Commit the transaction
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
