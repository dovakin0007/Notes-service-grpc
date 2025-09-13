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
	dbsql, mock, err := sqlmock.New()
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

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO actors")).
		WithArgs(in.Author.ID, in.Author.DisplayName, in.Author.AvatarURL).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO notes")).
		WithArgs(in.ID, in.ProjectID, in.Author.ID, in.Title, in.Content, false).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "author_id", "title", "content", "is_pinned", "created_at", "updated_at",
		}).AddRow(in.ID, in.ProjectID, in.Author.ID, in.Title, in.Content, false, now, now))

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO note_tags")).
		WithArgs(in.ID, "tag1", in.ID, "tag2").
		WillReturnResult(sqlmock.NewResult(1, 2))

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

func TestListNotes_ListAll(t *testing.T) {
	db, mock, err := newMockDatabase(t)
	defer err()

	filter := models.ListNotesFilter{
		PageSize: 15,
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

	for i := 0; i < 15; i++ {
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

	require.Len(t, notes, 15)
	require.NotEmpty(t, next)

	// Check ordering: because SortDesc==true and query ordered by n.updated_at DESC, the first note
	// should be >= second note in UpdatedAt.
	require.True(t, !notes[0].UpdatedAt.Before(notes[1].UpdatedAt), "expected notes[0].UpdatedAt >= notes[1].UpdatedAt")

	// ensure all expectations met
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestViewNote_Basic(t *testing.T) {
	d, mock, cleanup := newMockDatabase(t)
	defer cleanup()

	now := time.Now().UTC()
	noteID := "note-123"
	authorID := "actor-1"
	authorName := "Alice"
	avatarURL := "https://avatar/actor-1"

	cols := []string{
		"id", "project_id", "author_id", "title", "content", "is_pinned", "created_at", "updated_at",
		"author_display_name", "author_avatar_url", "tags",
	}

	rows := sqlmock.NewRows(cols).AddRow(
		noteID,
		"proj-1",
		authorID,
		"My title",
		"my content",
		false,
		now.Add(-time.Hour),
		now,
		authorName,
		avatarURL,
		"{tag1,tag2}",
	)

	queryRegex := `(?s)^SELECT .* FROM notes n .*LEFT JOIN .*note_tags.* ON t\.note_id = n\.id .*WHERE .*n\.id = .*`

	mock.ExpectQuery(queryRegex).WillReturnRows(rows)

	ctx := context.Background()
	note, err := d.ViewNote(ctx, noteID, models.GetNoteOptions{
		IncludeRevisions:   false,
		IncludeAttachments: false,
	})
	require.NoError(t, err)
	require.NotNil(t, note)
	require.Equal(t, noteID, note.ID)
	require.Equal(t, "My title", note.Title)
	require.Equal(t, ptrString("my content"), note.Content)
	require.NotNil(t, note.Author)
	require.Equal(t, authorID, note.Author.ID)
	require.Equal(t, &authorName, note.Author.DisplayName)
	require.Equal(t, &avatarURL, note.Author.AvatarURL)
	require.ElementsMatch(t, []string{"tag1", "tag2"}, note.Tags)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateNoteBasic(t *testing.T) {
	d, mock, cleanup := newMockDatabase(t)
	defer cleanup()

	now := time.Now().UTC()
	noteID := "note-123"
	authorID := "actor-1"
	authorName := "Alice"
	avatarURL := "https://avatar/actor-1"

	in := models.UpdateNoteInput{
		NoteID:   noteID,
		Title:    ptrString("Updated title"),
		Content:  ptrString("updated content"),
		IsPinned: ptrBool(true),
		Tags:     &[]string{"tag1", "tag2"},
		Attachments: []models.Attachment{
			{ID: "att-1", NoteID: noteID, URL: "https://files/att-1", FileName: "file.png", FileType: "image/png", UploadedAt: now, SHA256: ptrString("deadbeef"), SizeBytes: ptrInt64(1234)},
		},
	}
	mock.ExpectBegin()

	mock.ExpectQuery("UPDATE\\s+notes.*RETURNING\\s+id").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow(noteID),
	)

	// Expect tags delete
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM note_tags WHERE note_id=$1")).
		WithArgs(noteID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec("INSERT\\s+INTO\\s+note_tags").
		WillReturnResult(sqlmock.NewResult(1, 2))

	mock.ExpectExec("INSERT\\s+INTO\\s+attachments").
		WithArgs("att-1", noteID, "https://files/att-1", "file.png", "image/png", now, "deadbeef", int64(1234)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	cols := []string{
		"id", "project_id", "author_id", "title", "content", "is_pinned", "created_at", "updated_at",
		"author_display_name", "author_avatar_url", "tags",
	}
	rows := sqlmock.NewRows(cols).AddRow(
		noteID,
		"proj-1",
		authorID,
		"Updated title",
		"updated content",
		true,
		now.Add(-time.Hour),
		now,
		authorName,
		avatarURL,
		"{tag1,tag2}",
	)

	queryRegex := `(?s)^SELECT .* FROM notes n .*JOIN .*actors.* ON n\.author_id = a\.id .*LEFT JOIN .*note_tags.* ON .* WHERE n\.id = \$1`
	mock.ExpectQuery(queryRegex).WithArgs(noteID).WillReturnRows(rows)
	ctx := context.Background()

	note, err := d.UpdateNote(ctx, in)
	require.NoError(t, err)
	require.NotNil(t, note)
	require.Equal(t, noteID, note.ID)
	require.Equal(t, "Updated title", note.Title)
	require.ElementsMatch(t, []string{"tag1", "tag2"}, note.Tags)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteNote_Success(t *testing.T) {
	d, mock, cleanup := newMockDatabase(t)
	defer cleanup()

	noteID := "note-123"

	mock.ExpectBegin()
	var note_delete string = regexp.MustCompile(`DELETE\s+FROM\s+note_tags\s+WHERE\s+note_id\s*=\s*\$1`).String()
	mock.ExpectExec(note_delete).
		WithArgs(noteID).
		WillReturnResult(sqlmock.NewResult(0, 2))

	mock.ExpectExec(regexp.MustCompile(`DELETE\s+FROM\s+attachments\s+WHERE\s+note_id\s*=\s*\$1`).String()).
		WithArgs(noteID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.MustCompile(`DELETE\s+FROM\s+note_revisions\s+WHERE\s+note_id\s*=\s*\$1`).String()).
		WithArgs(noteID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectExec(regexp.MustCompile(`DELETE\s+FROM\s+notes\s+WHERE\s+id\s*=\s*\$1`).String()).
		WithArgs(noteID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	ok, err := d.DeleteNote(context.Background(), noteID, true)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, mock.ExpectationsWereMet())
}

func ptrString(s string) *string { return &s }

func ptrBool(b bool) *bool { return &b }

func ptrInt64(i int64) *int64 {
	return &i
}
