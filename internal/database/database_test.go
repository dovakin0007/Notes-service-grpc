package database_test

import (
	"context"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"dovakin0007.com/notes-grpc/internal/database"
	"dovakin0007.com/notes-grpc/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func newMockDatabase(t *testing.T) (*database.Database, sqlmock.Sqlmock, func()) {
	t.Helper()
	dbsql, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	sqlxDB := sqlx.NewDb(dbsql, "postgres")
	d := &database.Database{
		Mu: &sync.RWMutex{},
		Db: sqlxDB,
	}
	return d, mock, func() { sqlxDB.Close() }
}

func TestCreateNote_sqlmock(t *testing.T) {
	ctx := context.Background()
	d, mock, close := newMockDatabase(t)
	defer close()

	proj := "proj-1"
	content := "World"
	now := time.Now().UTC()
	var size int64 = 0

	in := models.CreateNoteInput{
		ID:        "note-1",
		ProjectID: &proj,
		Author: models.Actor{
			ID:          "actor-1",
			DisplayName: ptrString("Alice"),
			AvatarURL:   ptrString("https://avatar"),
		},
		Title:   "Hello",
		Content: &content,
		Tags:    []string{"tag1", "tag2"},
		Attachment: []models.Attachment{
			{
				ID:         "a1",
				NoteID:     "note-1",
				URL:        "http://u",
				FileName:   "f",
				FileType:   "txt",
				UploadedAt: now,
				SHA256:     nil,   // nil pointer (if SHA256 is *string) or use "" if it's a string
				SizeBytes:  &size, // ensure this matches the type in your model (int64)
			},
		},
	}

	mock.ExpectBegin()

	// Expect upsert actor - any SQL containing INSERT INTO actors
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO actors")).
		WithArgs(in.Author.ID, in.Author.DisplayName, in.Author.AvatarURL).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect insert into notes with RETURNING ... -> we must expect a Query (QueryRowContext)
	// The query is created by squirrel; match by prefix "INSERT INTO notes"
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO notes")).
		WithArgs(in.ID, in.ProjectID, in.Author.ID, in.Title, in.Content, false).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "author_id", "title", "content", "is_pinned", "created_at", "updated_at",
		}).AddRow(in.ID, in.ProjectID, in.Author.ID, in.Title, in.Content, false, now, now))

	// Expect insert into note_tags (two values)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO note_tags")).
		WithArgs(in.ID, "tag1", in.ID, "tag2").
		WillReturnResult(sqlmock.NewResult(1, 2))

	// Expect insert into attachments
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO attachments")).
		WithArgs("a1", in.ID, "http://u", "f", "txt", sqlmock.AnyArg(), nil, int64(0)). // depending on your struct zero-values
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	n, err := d.CreateNote(ctx, in)
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}
	if n.ID != in.ID {
		t.Fatalf("expected id %s got %s", in.ID, n.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

// TestListNotes_ListAll uses sqlmock to return multiple rows and verifies pagination token + ordering.
func TestListNotes_ListAll(t *testing.T) {
	db, mock, err := newMockDatabase(t)
	defer err()

	filter := models.ListNotesFilter{
		PageSize: 5,
		SortBy:   "updated_at",
		SortDesc: true,
	}

	cols := []string{
		"id", "project_id", "author_id", "title", "content", "is_pinned", "created_at", "updated_at",
		"author_id", "author_display_name", "author_avatar_url",
	}
	ctx := context.Background()
	rows := sqlmock.NewRows(cols)
	now := time.Now().UTC()

	for i := 0; i < 10; i++ {
		id := "note-" + strconv.Itoa(i)
		projectID := "proj-1"
		authorID := "actor-" + strconv.Itoa(i)
		title := "Title " + strconv.Itoa(i)
		content := "content " + strconv.Itoa(i)
		isPinned := false                                   // use bool instead of 0
		created := now.Add(-time.Duration(i) * time.Minute) // older for larger i
		updated := now.Add(-time.Duration(i) * time.Second) // newest when i==0

		rows.AddRow(id, projectID, authorID, title, content, isPinned, created, updated, authorID, "Author "+strconv.Itoa(i), "https://avatar/"+authorID)
	}

	// Expect a SELECT from notes with a LEFT JOIN actors and a LIMIT; use regexp to be flexible
	// The QueryMatcher is regexp, so build a regexp that matches a SELECT ... FROM notes ... LIMIT

	selectRegex := `(?s)^SELECT .* FROM notes .*LEFT JOIN actors(?:\s+a)? ON a\.id = n\.author_id .*LIMIT \d+`

	mock.ExpectQuery(selectRegex).WillReturnRows(rows)

	// Call ListNotes
	notes, next, err_1 := db.ListNotes(ctx, filter)
	if err_1 != nil {
		t.Fatalf("Error: %v", err_1)
	}
	require.NoError(t, err_1)

	// Since filter.PageSize was < 10, the method enforces 10 -> expect 10 records
	require.Len(t, notes, 10)

	// Since we returned full page (10) and ListNotes calculates next token when len==PageSize,
	// next should be non-empty.
	require.NotEmpty(t, next)

	// Check ordering: because SortDesc==true and query ordered by n.updated_at DESC, the first note
	// should be >= second note in UpdatedAt.
	require.True(t, !notes[0].UpdatedAt.Before(notes[1].UpdatedAt), "expected notes[0].UpdatedAt >= notes[1].UpdatedAt")

	// ensure all expectations met
	require.NoError(t, mock.ExpectationsWereMet())
}

func ptrString(s string) *string { return &s }
