package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"dovakin0007.com/notes-grpc/internal/models"
	"dovakin0007.com/notes-grpc/internal/utils"
	sq "github.com/Masterminds/squirrel"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	psql     = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	instance *Database
	once     sync.Once

	driverName = "postgres"
	user       = os.Getenv("POSTGRES_USER")
	password   = os.Getenv("POSTGRES_PASSWORD")
	dbName     = os.Getenv("POSTGRES_NAME")
	port       = os.Getenv("POSTGRES_PORT")
)

const ddl = `
-- 1) Basic reference table for ActorRef
CREATE TABLE IF NOT EXISTS actors (
    id            TEXT PRIMARY KEY,
    display_name  TEXT,
    avatar_url    TEXT
);

CREATE TABLE IF NOT EXISTS notes (
    id          TEXT PRIMARY KEY,
    project_id  TEXT,
    author_id   TEXT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    content     TEXT,
    is_pinned   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notes_set_updated_at ON notes;
CREATE TRIGGER trg_notes_set_updated_at
BEFORE UPDATE ON notes
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS note_tags (
    note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    tag      TEXT NOT NULL,
    PRIMARY KEY (note_id, tag)
);

CREATE TABLE IF NOT EXISTS note_revisions (
    id         TEXT PRIMARY KEY,
    note_id    TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    content    TEXT NOT NULL,
    editor_id  TEXT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    edited_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS attachments (
    id          TEXT PRIMARY KEY,
    note_id     TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    file_name   TEXT NOT NULL,
    file_type   TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sha256      TEXT,
    size_bytes  BIGINT
);

CREATE INDEX IF NOT EXISTS idx_notes_project_id    ON notes(project_id);
CREATE INDEX IF NOT EXISTS idx_notes_author_id     ON notes(author_id);
CREATE INDEX IF NOT EXISTS idx_notes_is_pinned     ON notes(is_pinned);

CREATE INDEX IF NOT EXISTS idx_notes_fts
ON notes
USING GIN (to_tsvector('english', coalesce(title,'') || ' ' || coalesce(content,'')));


CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag);
`

type Database struct {
	Mu *sync.RWMutex
	Db *sqlx.DB
}

func GetDb() *Database {
	once.Do(func() {
		instance = initDb()
	})
	return instance
}

func initDb() *Database {
	connection_str := fmt.Sprintf("user=%s password=%s dbname=%s port=%s sslmode=disable", user, password, dbName, port)

	Db := &Database{
		Mu: &sync.RWMutex{},
		Db: sqlx.MustConnect(driverName, connection_str),
	}

	Db.migrate(context.Background())
	return Db
}

func mustExecAll(ctx context.Context, Db *sqlx.DB, sql string) {
	if _, err := Db.ExecContext(ctx, sql); err != nil {
		panic(fmt.Errorf("migration failed: %w", err))
	}
}

func (d *Database) migrate(ctx context.Context) {

	mustExecAll(ctx, d.Db, ddl)
}

func (d *Database) Close() error {
	if d.Db != nil {
		return d.Db.Close()
	}
	return nil
}

// For TESTING purposes only
func ResetDb() {
	if instance != nil && instance.Db != nil {
		instance.Close()
		instance = nil
		once = sync.Once{}
	}
}

func (d *Database) UpsertActor(ctx context.Context, actor models.Actor) error {
	d.Mu.Lock()
	defer d.Mu.Unlock()
	q := psql.Insert("actors").
		Columns("id", "display_name", "avatar_url").
		Values(actor.ID, actor.DisplayName, actor.AvatarURL).
		Suffix("ON CONFLICT (id) DO UPDATE SET display_name=EXCLUDED.display_name, avatar_url=EXCLUDED.avatar_url")

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return err
	}
	_, err = d.Db.ExecContext(ctx, sqlStr, args...)
	return err
}

func (d *Database) CreateNote(ctx context.Context, in models.CreateNoteInput) (*models.Note, error) {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	tx, err := d.Db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		tx.Rollback()
	}()
	aq := psql.Insert("actors").
		Columns("id", "display_name", "avatar_url").
		Values(in.Author.ID, in.Author.DisplayName, in.Author.AvatarURL).
		Suffix("ON CONFLICT (id) DO UPDATE SET display_name=EXCLUDED.display_name, avatar_url=EXCLUDED.avatar_url")
	var query, args, sql_err = aq.ToSql()
	if sql_err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return nil, err
	}

	nq := psql.Insert("notes").
		Columns("id", "project_id", "author_id", "title", "content", "is_pinned").
		Values(in.ID, in.ProjectID, in.Author.ID, in.Title, in.Content, false).
		Suffix("RETURNING id, project_id, author_id, title, content, is_pinned, created_at, updated_at")
	query, args, sql_err = nq.ToSql()
	if sql_err != nil {
		return nil, err
	}
	var n models.Note
	if err := tx.GetContext(ctx, &n, query, args...); err != nil {
		return nil, err
	}

	if len(in.Tags) > 0 {
		tq := psql.Insert("note_tags").Columns("note_id", "tag")
		for _, t := range in.Tags {
			tq = tq.Values(n.ID, t)
		}
		tq = tq.Suffix("ON CONFLICT DO NOTHING")
		query, args, sql_err = tq.ToSql()
		if sql_err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}
	err = InsertAttachment(ctx, tx, in.Attachment)

	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	n.Author = &in.Author
	n.Tags = append([]string(nil), in.Tags...)
	return &n, nil
}

func (d *Database) ViewNote(ctx context.Context, noteID string, opts models.GetNoteOptions) (*models.Note, error) {
	d.Mu.RLock()
	defer d.Mu.RUnlock()
	var q = psql.Select("n.id", "n.project_id", "n.author_id", "n.title", "n.content", "n.is_pinned", "n.created_at", "n.updated_at",
		"a.id AS \"author.id\"", "a.display_name AS \"author.display_name\"", "a.avatar_url AS \"author.avatar_url\"",
		"COALESCE(t.tags, '{}') AS tags").From("notes n").
		Join("actors a ON n.author_id = a.id").
		LeftJoin(`(
          SELECT note_id, ARRAY_AGG(tag ORDER BY tag) AS tags
          FROM note_tags
          WHERE note_id = $1
          GROUP BY note_id
        ) t ON t. = n.id`).
		Where(sq.Eq{"n.id": noteID})
	if opts.IncludeRevisions {
		q = q.LeftJoin(`
            SELECT r.id, r.note_id, r.title, r.content, r.editor_id, r.edited_at,
                   e.id AS "editor.id", e.display_name AS "editor.display_name", e.avatar_url AS "editor.avatar_url"
            FROM note_revisions r
            JOIN actors e ON e.id = r.editor_id
            WHERE r.note_id = $1
            ORDER BY r.edited_at DESC, r.id DESC
        `)
	}

	if opts.IncludeAttachments {
		q = q.LeftJoin(`
            SELECT a.id, a.note_id, a.url, a.file_name, a.file_type, a.uploaded_at, a.sha256, a.size_bytes
            FROM attachments a
            WHERE a.note_id = $1
            ORDER BY a.uploaded_at DESC, a.id DESC
        `)
	}

	query, args, sql_err := q.ToSql()
	if sql_err != nil {
		return nil, sql_err
	}

	type row struct {
		models.Note
		AuthorID_       string  `Db:"author.id"`
		AuthorName      *string `Db:"author.display_name"`
		AuthorAvatarURL *string `Db:"author.avatar_url"`
	}
	var rw row
	if err := d.Db.GetContext(ctx, &rw, query, args...); err != nil {
		return nil, err
	}

	n := rw.Note
	n.Author = &models.Actor{
		ID:          rw.AuthorID_,
		DisplayName: rw.AuthorName,
		AvatarURL:   rw.AuthorAvatarURL,
	}
	return &rw.Note, nil

}

func (d *Database) ListNotes(ctx context.Context, filter models.ListNotesFilter) ([]models.Note, string, error) {

	if filter.PageSize < 10 {
		filter.PageSize = 10
	} else if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	var sortBy string

	switch strings.ToLower(filter.SortBy) {
	case "updated_at", "created_at", "title", "is_pinned":
		sortBy = filter.SortBy

	default:
		sortBy = "updated_at"
	}

	dir := "DESC"
	if !filter.SortDesc {
		dir = "ASC"
	}

	q := psql.Select(
		"n.id", "n.project_id", "n.author_id", "n.title", "n.content", "n.is_pinned", "n.created_at", "n.updated_at",
		"a.id AS author_id", "a.display_name AS author_display_name", "a.avatar_url AS author_avatar_url",
	).
		From("notes n").
		LeftJoin("actors a ON a.id = n.author_id"). // change to your author table name
		OrderBy(fmt.Sprintf("n.%s %s, n.id %s", sortBy, dir, dir)).
		Limit(uint64(filter.PageSize))

	if filter.ProjectID != nil {
		q = q.Where(sq.Eq{"n.project_id": *filter.ProjectID})
	}
	if filter.UserID != nil {
		q = q.Where(sq.Eq{"n.author_id": filter.UserID})
	}
	if filter.Query != nil && *filter.Query != "" {
		q = q.Where("to_tsvector('english', coalesce(n.title,'') || ' ' || coalesce(n.content,'')) @@ plainto_tsquery('english', ?)", filter.Query)
	}

	if filter.PageToken != "" {
		if c, err := utils.DecodePaginationToken(filter.PageToken); err == nil {
			op := "<"
			if dir == "ASC" {
				op = ">"
			}

			// Keyset pagination filter:
			//   (1) Include rows where n.<sortBy> is after the cursor value (c.Key),
			//   OR
			//   (2) If n.<sortBy> equals c.Key, use n.id as a tiebreaker to ensure stable ordering.
			// This avoids duplicates and gaps compared to OFFSET/LIMIT pagination.
			q = q.Where(fmt.Sprintf("(n.%s %s ? OR (n.%s = ? AND n.id %s ?))", sortBy, op, sortBy, op), c.Key, c.Key, c.ID)
		}
	}
	type row struct {
		models.Note
		AuthorID        string  `db:"author_id"` // if you already have author_id in models.Note, drop this
		AuthorName      *string `db:"author_display_name"`
		AuthorAvatarURL *string `db:"author_avatar_url"`
	}
	var rows []row
	sqlStr, args, err := q.ToSql()

	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to build query: %v", err)
	}

	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, "", status.Errorf(codes.Canceled, "request canceled")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, "", status.Errorf(codes.DeadlineExceeded, "deadline exceeded")
		}
	}

	if err := d.Db.SelectContext(ctx, &rows, sqlStr, args...); err != nil {
		return nil, "", status.Errorf(codes.Internal, "query failed: %v", err)
	}
	notes := make([]models.Note, 0, len(rows))
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, "", status.Errorf(codes.Canceled, "request canceled")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, "", status.Errorf(codes.DeadlineExceeded, "deadline exceeded")
		}
		return nil, "", status.Errorf(codes.Internal, "context done: %v", ctx.Err())
	default:

	}
	for _, row := range rows {
		n := row.Note
		n.Author = &models.Actor{ID: row.AuthorID, DisplayName: row.AuthorName, AvatarURL: row.AuthorAvatarURL}
		notes = append(notes, n)
	}

	var next string
	if len(notes) == filter.PageSize {
		last := notes[len(notes)-1]
		var key string
		var keyType string

		switch sortBy {
		case "updated_at":
			key = last.UpdatedAt.UTC().Format(time.RFC3339Nano)
			keyType = "time"
		case "created_at":
			key = last.CreatedAt.UTC().Format(time.RFC3339Nano)
			keyType = "time"
		case "title":
			key = last.Title
			keyType = "string"
		case "is_pinned":
			key = strconv.FormatBool(last.IsPinned)
			keyType = "bool"
		default:
			key = last.UpdatedAt.UTC().Format(time.RFC3339Nano)
			keyType = "time"
		}
		cur := utils.NotesPagination{
			Key:       key,
			KeyType:   keyType,
			ID:        last.ID,
			SortBy:    sortBy,
			Direction: dir,
		}
		if s, err := utils.EncodePaginationToken(cur); err == nil {
			next = s
		} else {
			next = ""
		}
	}
	return notes, next, nil
}

func (d *Database) UpdateNote(ctx context.Context, in models.UpdateNoteInput) (*models.Note, error) {
	d.Mu.Lock()
	defer d.Mu.Unlock()
	tx, err := d.Db.BeginTxx(ctx, &sql.TxOptions{})
	defer func() {
		tx.Rollback()
	}()
	if err != nil {
		return nil, err
	}
	uq := psql.Update("notes")

	if in.Title != nil {
		uq = uq.Set("title", *in.Title)
	}
	if in.Content != nil {
		uq = uq.Set("content", *in.Content)
	}
	if in.IsPinned != nil {
		uq = uq.Set("is_pinned", *in.IsPinned)
	}

	if in.IfMatchUpdatedAt != nil {
		uq = uq.Where(sq.And{sq.Eq{"id": in.NoteID}, sq.Eq{"updated_at": *in.IfMatchUpdatedAt}})
	} else {
		uq = uq.Where(sq.Eq{"id": in.NoteID})
	}
	uq = uq.Suffix("RETURNING id")
	if sqlStr, args, err := uq.ToSql(); err == nil && len(args) > 0 {
		var id string
		if err := tx.GetContext(ctx, &id, sqlStr, args...); err != nil && errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	if in.Tags != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id=$1`, in.NoteID); err != nil {
			return nil, err
		}
		if len(*in.Tags) > 0 {
			tq := psql.Insert("note_tags").Columns("note_id", "tag")
			for _, t := range *in.Tags {
				tq = tq.Values(in.NoteID, t)
			}
			query, args, err := tq.ToSql()
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return nil, err
			}
		}
	}

	if len(in.Attachments) > 0 {
		for _, a := range in.Attachments {
			q := psql.Insert("attachments").Columns("id", "note_id", "url", "file_name", "file_type", "uploaded_at", "sha256", "size_bytes").
				Values(a.ID, a.NoteID, a.URL, a.FileName, a.FileType, a.UploadedAt, a.SHA256, a.SizeBytes).
				Suffix(`ON CONFLICT (id) DO UPDATE SET 
                    url=EXCLUDED.url, file_name=EXCLUDED.file_name, file_type=EXCLUDED.file_type,
                    uploaded_at=EXCLUDED.uploaded_at, sha256=EXCLUDED.sha256, size_bytes=EXCLUDED.size_bytes`)
			if query, args, err := q.ToSql(); err == nil {
				tx.ExecContext(ctx, query, args...)
			} else {
				return nil, err
			}

		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.ViewNote(ctx, in.NoteID, models.GetNoteOptions{IncludeRevisions: false, IncludeAttachments: true})

}

func (d *Database) DeleteNote(ctx context.Context, id string, hardDel bool) (bool, error) {
	d.Mu.Lock()
	defer d.Mu.Unlock()
	tx, err := d.Db.BeginTxx(ctx, &sql.TxOptions{})
	defer func() {
		tx.Rollback()
	}()
	if err != nil {
		return false, fmt.Errorf("enable to start a transaction %s", err.Error())
	}
	if true {
		childTables := []string{"note_tags", "attachments", "note_revisions"}

		for _, t := range childTables {
			q, args, err := psql.Delete(t).
				Where(sq.Eq{"note_id": id}).
				ToSql()
			if err != nil {
				return false, fmt.Errorf("building delete for %s: %w", t, err)
			}
			if _, err := tx.ExecContext(ctx, q, args...); err != nil {
				return false, fmt.Errorf("deleting from %s: %w", t, err)
			}
		}

		delQ, delArgs, err := psql.Delete("notes").
			Where(sq.Eq{"id": id}).
			ToSql()
		if err != nil {
			return false, fmt.Errorf("building delete for notes: %w", err)
		}
		res, err := tx.ExecContext(ctx, delQ, delArgs...)
		if err != nil {
			return false, fmt.Errorf("deleting note: %w", err)
		}
		ra, err := res.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("rows affected (notes delete): %w", err)
		}
		if ra == 0 {
			if err := tx.Commit(); err != nil {
				return false, fmt.Errorf("commit failed after no-op delete: %w", err)
			}
			return false, nil
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit failed after delete: %w", err)
		}

	}
	return true, nil

}

func InsertAttachment(ctx context.Context, tx *sqlx.Tx, attachment []models.Attachment) error {
	if len(attachment) > 0 {
		for _, a := range attachment {
			q := psql.Insert("attachments").Columns("id", "note_id", "url", "file_name", "file_type", "uploaded_at", "sha256", "size_bytes").
				Values(a.ID, a.NoteID, a.URL, a.FileName, a.FileType, a.UploadedAt, a.SHA256, a.SizeBytes).
				Suffix(`ON CONFLICT (id) DO UPDATE SET 
                    url=EXCLUDED.url, file_name=EXCLUDED.file_name, file_type=EXCLUDED.file_type,
                    uploaded_at=EXCLUDED.uploaded_at, sha256=EXCLUDED.sha256, size_bytes=EXCLUDED.size_bytes`)
			query, args, err := q.ToSql()
			if err != nil {
				return fmt.Errorf("build attachments insert: %w", err)
			}

			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("exec attachments insert: %w", err)
			}

		}
	}
	return nil
}
