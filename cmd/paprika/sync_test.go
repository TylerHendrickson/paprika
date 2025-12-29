package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TylerHendrickson/paprika"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

func newMockClient(t *testing.T, server *httptest.Server) *paprika.Client {
	t.Helper()
	baseURL, err := url.Parse(server.URL + "/")
	require.NoError(t, err)
	client, err := paprika.NewClientWithURL("user", "pass", baseURL)
	require.NoError(t, err)
	return client
}

func TestNumWorkersValidate(t *testing.T) {
	require.NoError(t, NumWorkers(1).Validate())
	require.EqualError(t, NumWorkers(0).Validate(), "must be at least 1 worker")
}

func TestPurgeAfterUnmarshalText(t *testing.T) {
	var p PurgeAfter
	require.NoError(t, p.UnmarshalText([]byte("2h30m")))
	assert.Equal(t, PurgeAfter(150*time.Minute), p)

	err := p.UnmarshalText([]byte("-5m"))
	require.EqualError(t, err, "duration cannot be negative")
}

func TestPurgeAfterString(t *testing.T) {
	assert.Equal(t, "<never>", (*PurgeAfter)(nil).String())

	var zero PurgeAfter
	assert.Equal(t, "<immediate>", zero.String())

	p := PurgeAfter(3 * time.Hour)
	assert.Equal(t, "3h0m0s", p.String())
}

func TestSaveCategoriesIndex(t *testing.T) {
	tempDir := t.TempDir()
	cli := &CLI{DataDir: tempDir}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/categories", r.URL.Path)
		_, _ = w.Write([]byte(`{"result":[{"uid":"cat1","name":"Breakfast"}]}`))
	}))
	defer server.Close()

	client := newMockClient(t, server)

	cmd := SyncCMD{}
	err := cmd.SaveCategoriesIndex(context.Background(), cli, client, newTestLogger())
	require.NoError(t, err)

	data, err := os.ReadFile(pathToCategoriesIndexFile(tempDir))
	require.NoError(t, err)

	var categories []paprika.Category
	require.NoError(t, json.Unmarshal(data, &categories))
	assert.Equal(t, []paprika.Category{{UID: "cat1", Name: "Breakfast"}}, categories)
}

func TestSaveRecipesIndex(t *testing.T) {
	tempDir := t.TempDir()
	cli := &CLI{DataDir: tempDir}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/recipes", r.URL.Path)
		_, _ = w.Write([]byte(`{"result":[{"uid":"abcde","hash":"h1"},{"uid":"fghij","hash":"h2"}]}`))
	}))
	defer server.Close()

	client := newMockClient(t, server)

	cmd := SyncCMD{}
	items, err := cmd.SaveRecipesIndex(context.Background(), cli, client, newTestLogger())
	require.NoError(t, err)
	assert.Len(t, items, 2)

	data, err := os.ReadFile(pathToRecipesIndexFile(tempDir))
	require.NoError(t, err)

	var index []paprika.RecipeItem
	require.NoError(t, json.Unmarshal(data, &index))
	assert.Equal(t, items, index)
}

func TestUpsertRecipe(t *testing.T) {
	t.Run("createNewRecipe", func(t *testing.T) {
		tempDir := t.TempDir()
		cli := &CLI{DataDir: tempDir}

		recipe := paprika.Recipe{UID: "abcdef", Hash: "newhash", Name: "Soup"}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/recipe/abcdef", r.URL.Path)
			_, _ = w.Write([]byte(`{"result":{"uid":"abcdef","hash":"newhash","name":"Soup"}}`))
		}))
		defer server.Close()

		client := newMockClient(t, server)

		cmd := SyncCMD{}
		saved, err := cmd.UpsertRecipe(context.Background(), cli, client, paprika.RecipeItem{UID: recipe.UID, Hash: recipe.Hash}, newTestLogger())
		require.NoError(t, err)
		assert.True(t, saved)

		data, err := os.ReadFile(pathToRecipeJSONFile(tempDir, recipe.UID))
		require.NoError(t, err)

		var stored paprika.Recipe
		require.NoError(t, json.Unmarshal(data, &stored))
		assert.Equal(t, recipe, stored)
	})

	t.Run("skipWhenHashesMatch", func(t *testing.T) {
		tempDir := t.TempDir()
		cli := &CLI{DataDir: tempDir}
		uid := "skip01"

		require.NoError(t, saveAsJSON(paprika.Recipe{UID: uid, Hash: "h1"}, pathToRecipeJSONFile(tempDir, uid)))

		var recipeRequests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recipeRequests.Add(1)
			_, _ = w.Write([]byte(`{"result":{"uid":"skip01","hash":"h1"}}`))
		}))
		defer server.Close()

		client := newMockClient(t, server)

		cmd := SyncCMD{}
		saved, err := cmd.UpsertRecipe(context.Background(), cli, client, paprika.RecipeItem{UID: uid, Hash: "h1"}, newTestLogger())
		require.NoError(t, err)
		assert.False(t, saved)
		assert.Equal(t, int64(0), recipeRequests.Load())
	})

	t.Run("updateWhenHashDiffers", func(t *testing.T) {
		tempDir := t.TempDir()
		cli := &CLI{DataDir: tempDir}
		uid := "updat3"

		require.NoError(t, saveAsJSON(paprika.Recipe{UID: uid, Hash: "old"}, pathToRecipeJSONFile(tempDir, uid)))

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/recipe/"+uid, r.URL.Path)
			_, _ = w.Write([]byte(`{"result":{"uid":"updat3","hash":"new"}}`))
		}))
		defer server.Close()

		client := newMockClient(t, server)

		cmd := SyncCMD{}
		saved, err := cmd.UpsertRecipe(context.Background(), cli, client, paprika.RecipeItem{UID: uid, Hash: "new"}, newTestLogger())
		require.NoError(t, err)
		assert.True(t, saved)

		data, err := os.ReadFile(pathToRecipeJSONFile(tempDir, uid))
		require.NoError(t, err)
		assert.Contains(t, string(data), `"hash":"new"`)
	})

	t.Run("errorOnMismatchedUID", func(t *testing.T) {
		tempDir := t.TempDir()
		cli := &CLI{DataDir: tempDir}
		uid := "badid"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"result":{"uid":"other","hash":"h1"}}`))
		}))
		defer server.Close()

		client := newMockClient(t, server)

		cmd := SyncCMD{}
		saved, err := cmd.UpsertRecipe(context.Background(), cli, client, paprika.RecipeItem{UID: uid, Hash: "h1"}, newTestLogger())
		require.Error(t, err)
		assert.False(t, saved)
		assert.Contains(t, err.Error(), "does not match requested UID")
	})
}

func TestShouldSaveRecipe(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "recipe.json")
	log := newTestLogger()

	t.Run("missingFile", func(t *testing.T) {
		update, exists := shouldSaveRecipe(path, "h1", log)
		assert.True(t, update)
		assert.False(t, exists)
	})

	t.Run("invalidJSON", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte("{not-json"), 0644))
		update, exists := shouldSaveRecipe(path, "h2", log)
		assert.True(t, update)
		assert.True(t, exists)
	})

	t.Run("matchingHash", func(t *testing.T) {
		require.NoError(t, saveAsJSON(paprika.Recipe{UID: "abc", Hash: "h3"}, path))
		update, exists := shouldSaveRecipe(path, "h3", log)
		assert.False(t, update)
		assert.True(t, exists)
	})

	t.Run("differentHash", func(t *testing.T) {
		require.NoError(t, saveAsJSON(paprika.Recipe{UID: "abc", Hash: "old"}, path))
		update, exists := shouldSaveRecipe(path, "new", log)
		assert.True(t, update)
		assert.True(t, exists)
	})
}

func TestSaveAsJSON(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "nested", "file.json")

	err := saveAsJSON(map[string]string{"k": "v"}, targetPath)
	require.NoError(t, err)

	data, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"k":"v"`)
}

func TestPurgeUnreferencedRecipes(t *testing.T) {
	now := time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)

	t.Run("purgesExpiredUnindexedRecipe", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, saveAsJSON([]paprika.RecipeItem{{UID: "keep1", Hash: "h1"}}, pathToRecipesIndexFile(tempDir)))

		uid := "old11"
		recipeDir := pathToRecipeDir(tempDir, uid)
		require.NoError(t, os.MkdirAll(recipeDir, 0755))
		require.NoError(t, os.WriteFile(pathToRecipeJSONFile(tempDir, uid), []byte(`{"uid":"old11","hash":"old"}`), 0644))
		require.NoError(t, os.WriteFile(pathToRecipeDeleteMarkerFile(tempDir, uid), []byte(now.Add(-48*time.Hour).Format(time.RFC3339Nano)), 0644))

		err := purgeUnreferencedRecipes(context.Background(), tempDir, now, 24*time.Hour, newTestLogger())
		require.NoError(t, err)

		_, err = os.Stat(recipeDir)
		require.True(t, os.IsNotExist(err))
	})

	t.Run("createsMarkerForNewUnindexedRecipe", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, saveAsJSON([]paprika.RecipeItem{}, pathToRecipesIndexFile(tempDir)))

		uid := "new22"
		recipeDir := pathToRecipeDir(tempDir, uid)
		require.NoError(t, os.MkdirAll(recipeDir, 0755))
		require.NoError(t, os.WriteFile(pathToRecipeJSONFile(tempDir, uid), []byte(`{"uid":"new22","hash":"h"}`), 0644))

		err := purgeUnreferencedRecipes(context.Background(), tempDir, now, time.Hour, newTestLogger())
		require.NoError(t, err)

		markerPath := pathToRecipeDeleteMarkerFile(tempDir, uid)
		data, err := os.ReadFile(markerPath)
		require.NoError(t, err)

		markerTime, err := time.Parse(time.RFC3339Nano, string(data))
		require.NoError(t, err)
		assert.Equal(t, now, markerTime)
	})

	t.Run("retainsUnexpiredMarker", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, saveAsJSON([]paprika.RecipeItem{}, pathToRecipesIndexFile(tempDir)))

		uid := "recent3"
		recipeDir := pathToRecipeDir(tempDir, uid)
		require.NoError(t, os.MkdirAll(recipeDir, 0755))
		marker := now.Add(-10 * time.Minute).Format(time.RFC3339Nano)
		require.NoError(t, os.WriteFile(pathToRecipeDeleteMarkerFile(tempDir, uid), []byte(marker), 0644))

		err := purgeUnreferencedRecipes(context.Background(), tempDir, now, time.Hour, newTestLogger())
		require.NoError(t, err)

		_, err = os.Stat(recipeDir)
		require.NoError(t, err)
	})

	t.Run("removesStaleMarkerForIndexedRecipe", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, saveAsJSON([]paprika.RecipeItem{{UID: "keepm", Hash: "h1"}}, pathToRecipesIndexFile(tempDir)))

		recipeDir := pathToRecipeDir(tempDir, "keepm")
		require.NoError(t, os.MkdirAll(recipeDir, 0755))
		require.NoError(t, os.WriteFile(pathToRecipeDeleteMarkerFile(tempDir, "keepm"), []byte(now.Add(-time.Hour).Format(time.RFC3339Nano)), 0644))

		err := purgeUnreferencedRecipes(context.Background(), tempDir, now, time.Hour, newTestLogger())
		require.NoError(t, err)

		_, err = os.Stat(pathToRecipeDeleteMarkerFile(tempDir, "keepm"))
		require.True(t, os.IsNotExist(err))
	})

	t.Run("immediatePurgeWithoutMarker", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, saveAsJSON([]paprika.RecipeItem{}, pathToRecipesIndexFile(tempDir)))

		uid := "now44"
		recipeDir := pathToRecipeDir(tempDir, uid)
		require.NoError(t, os.MkdirAll(recipeDir, 0755))
		require.NoError(t, os.WriteFile(pathToRecipeJSONFile(tempDir, uid), []byte(`{"uid":"now44"}`), 0644))

		err := purgeUnreferencedRecipes(context.Background(), tempDir, now, 0, newTestLogger())
		require.NoError(t, err)

		_, err = os.Stat(recipeDir)
		require.True(t, os.IsNotExist(err))
	})
}

func TestReadTimestampMarker(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "marker")
	expected := time.Date(2023, 5, 6, 7, 8, 9, 0, time.UTC)
	require.NoError(t, os.WriteFile(target, []byte(expected.Format(time.RFC3339Nano)), 0644))

	got, err := readTimestampMarker(target, time.RFC3339Nano)
	require.NoError(t, err)
	assert.True(t, expected.Equal(got))
}

func TestPruneFilelessSubtrees(t *testing.T) {
	tempDir := t.TempDir()
	keepDir := filepath.Join(tempDir, "keep", "child")
	removeDir := filepath.Join(tempDir, "remove", "empty", "nested")

	require.NoError(t, os.MkdirAll(keepDir, 0755))
	require.NoError(t, os.MkdirAll(removeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(keepDir, "file.txt"), []byte("data"), 0644))

	err := PruneFilelessSubtrees(context.Background(), tempDir)
	require.NoError(t, err)

	_, err = os.Stat(keepDir)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tempDir, "remove"))
	require.True(t, os.IsNotExist(err))
}

func TestSyncRunSuccess(t *testing.T) {
	tempDir := t.TempDir()
	cli := &CLI{DataDir: tempDir}
	purgeAfter := PurgeAfter(10 * time.Millisecond)
	cmd := SyncCMD{
		IncludeRecipes:      true,
		IncludeCategories:   true,
		DownloadConcurrency: 2,
		PurgeAfter:          &purgeAfter,
	}

	recipeIndex := []paprika.RecipeItem{
		{UID: "abcde", Hash: "h1"},
		{UID: "vwxyz", Hash: "h2"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/categories":
			_, _ = w.Write([]byte(`{"result":[{"uid":"cat1","name":"Lunch"}]}`))
		case "/recipes":
			_, _ = w.Write([]byte(`{"result":[{"uid":"abcde","hash":"h1"},{"uid":"vwxyz","hash":"h2"}]}`))
		case "/recipe/abcde":
			_, _ = w.Write([]byte(`{"result":{"uid":"abcde","hash":"h1","name":"First"}}`))
		case "/recipe/vwxyz":
			_, _ = w.Write([]byte(`{"result":{"uid":"vwxyz","hash":"new-hash","name":"Second"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newMockClient(t, server)

	// Pre-existing unindexed recipe with old marker should be purged.
	oldUID := "old11"
	oldDir := pathToRecipeDir(tempDir, oldUID)
	require.NoError(t, os.MkdirAll(oldDir, 0755))
	require.NoError(t, os.WriteFile(pathToRecipeJSONFile(tempDir, oldUID), []byte(`{"uid":"old11"}`), 0644))
	require.NoError(t, os.WriteFile(pathToRecipeDeleteMarkerFile(tempDir, oldUID), []byte(time.Now().Add(-time.Hour).Format(time.RFC3339Nano)), 0644))

	err := cmd.Run(context.Background(), cli, client, newTestLogger())
	require.NoError(t, err)

	for _, item := range recipeIndex {
		_, err := os.Stat(pathToRecipeJSONFile(tempDir, item.UID))
		require.NoError(t, err)
	}

	// Old unindexed recipe should be removed and pruned.
	_, err = os.Stat(oldDir)
	require.True(t, os.IsNotExist(err))

	// Categories and recipes index files should exist.
	_, err = os.Stat(pathToCategoriesIndexFile(tempDir))
	require.NoError(t, err)
	_, err = os.Stat(pathToRecipesIndexFile(tempDir))
	require.NoError(t, err)
}

func TestSyncRunWithErrors(t *testing.T) {
	tempDir := t.TempDir()
	cli := &CLI{DataDir: tempDir}
	cmd := SyncCMD{
		IncludeRecipes:      true,
		IncludeCategories:   false,
		DownloadConcurrency: 1,
	}

	// Return error for recipes index to trigger exitWithErrors.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/recipes") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`boom`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newMockClient(t, server)

	err := cmd.Run(context.Background(), cli, client, newTestLogger())
	require.EqualError(t, err, "sync completed with errors")
}
