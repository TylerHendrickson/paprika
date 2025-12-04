package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TylerHendrickson/paprika"
	"github.com/rs/zerolog"
)

type NumWorkers int

func (i NumWorkers) Validate() error {
	if i <= 0 {
		return fmt.Errorf("must be at least 1 worker")
	}
	return nil
}

// Sync is the sub-command for backing up Paprika data.
type SyncCMD struct {
	IncludeRecipes      bool          `help:"Whether to sync include recipes." negatable:"" default:"true" env:"PAPRIKA_SYNC_RECIPES"`
	PurgeAfter          time.Duration `help:"Specifies the duration after which recipes not found in Paprika are purged. Negative values disable purge checks." default:"-1s" env:"PAPRIKA_SYNC_PURGE_AFTER" placeholder:"DURATION"`
	IncludeCategories   bool          `help:"Whether to sync categories." negatable:"" default:"true" env:"PAPRIKA_SYNC_CATEGORIES"`
	DownloadConcurrency NumWorkers    `help:"Maximum concurrent recipe downloads." default:"10" env:"PAPRIKA_SYNC_WORKERS"`
}

func (cmd *SyncCMD) Run(ctx context.Context, cli *CLI, pc *paprika.Client, log zerolog.Logger) error {
	var exitWithErrors atomic.Bool
	wg := sync.WaitGroup{}

	if cmd.IncludeCategories {
		wg.Go(func() {
			if cmd.SaveCategoriesIndex(ctx, cli, pc, log) != nil {
				exitWithErrors.Store(true)
			}
		})
	}

	var savedRecipesCount atomic.Int64
	if cmd.IncludeRecipes {
		recipesQueue := make(chan paprika.RecipeItem, cmd.DownloadConcurrency)
		wg.Go(func() {
			defer close(recipesQueue)

			recipeIndexItems, err := cmd.SaveRecipesIndex(ctx, cli, pc, log)
			if err != nil {
				exitWithErrors.Store(true)
			}
			var itemsQueued int
			for _, item := range recipeIndexItems {
				recipesQueue <- item
				itemsQueued++
			}
			log.Debug().Int("total-items", itemsQueued).
				Msg("added all indexed recipe items to sync queue")
		})

		for i := range cmd.DownloadConcurrency {
			wg.Go(func() {
				log := log.With().Int("worker-id", int(i)+1).Logger()
				var workerSavedRecipesCount int64
				defer func() {
					if workerSavedRecipesCount > 0 {
						log.Debug().
							Int64("saved-recipes-count", workerSavedRecipesCount).
							Msg("worker saved recipes in queue")
						savedRecipesCount.Add(workerSavedRecipesCount)
					} else {
						log.Debug().Msg("worker stopped before saving any recipes")
					}
				}()

				for {
					// Prioritize context cancellation
					select {
					case <-ctx.Done():
						log.Warn().Err(ctx.Err()).
							Str("reason", "shutdown requested").
							Msg("shutting down worker")
						return
					default:
					}

					select {
					case <-ctx.Done():
						log.Warn().Err(ctx.Err()).
							Str("reason", "shutdown requested").
							Msg("shutting down worker")
						return
					case ref, ok := <-recipesQueue:
						if !ok {
							log.Debug().Str("reason", "no more work").
								Msg("shutting down worker")
							return
						}
						log := log.With().
							Str("recipe-uid", ref.UID).
							Str("recipe-indexed-hash", ref.Hash).Logger()
						saved, err := cmd.UpsertRecipe(ctx, cli, pc, ref, log)
						if err != nil {
							exitWithErrors.Store(true)
							log.Err(err).Msg("worker task failed for recipe item in queue")
						}
						if saved {
							workerSavedRecipesCount++
						}
					}
				}
			})
		}
	}

	wg.Wait()
	if cmd.IncludeRecipes {
		log.Info().Int64("total-saved", savedRecipesCount.Load()).
			Msg("saved new/updated recipes")
	}

	if !exitWithErrors.Load() && cmd.PurgeAfter >= 0 {
		if err := cmd.PurgeUnreferencedRecipes(ctx, cli.DataDir, time.Now(), log); err != nil {
			log.Err(err).Msg("error purging unindexed recipes")
			exitWithErrors.Store(true)
		} else {
			pruneRoot := pathToRecipesDir(cli.DataDir)
			if err := PruneFilelessSubtrees(ctx, pruneRoot); err != nil {
				log.Err(err).
					Str("recipes-data-root", pruneRoot).
					Msg("error pruning empty directories under recipes data root")
				exitWithErrors.Store(true)
			}
		}
	}

	if exitWithErrors.Load() {
		return fmt.Errorf("sync completed with errors")
	}
	log.Info().Msg("sync completed successfully")
	return nil
}

func (cmd *SyncCMD) SaveCategoriesIndex(ctx context.Context, cli *CLI, c *paprika.Client, log zerolog.Logger) error {
	categories, err := c.Categories(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get categories from Paprika API")
		return err
	}

	path := pathToCategoriesIndexFile(cli.DataDir)
	log = log.With().Str("categories-index-file", path).Logger()
	if err := saveAsJSON(categories, path); err != nil {
		log.Err(err).Msg("error saving Paprika categories index file")
		return err
	}
	log.Info().Msg("saved Paprika categories index file")
	return nil
}

func (cmd *SyncCMD) SaveRecipesIndex(ctx context.Context, cli *CLI, c *paprika.Client, log zerolog.Logger) ([]paprika.RecipeItem, error) {
	recipesIndex, err := c.Recipes(ctx)
	if err != nil {
		log.Err(err).Msg("failed to fetch Paprika recipes index")
		return recipesIndex, err
	}

	path := pathToRecipesIndexFile(cli.DataDir)
	log = log.With().Str("path", path).Logger()
	err = saveAsJSON(recipesIndex, path)
	if err != nil {
		log.Err(err).Msg("save to create Paprika recipes index file")
	} else {
		log.Info().Msg("saved Paprika recipes index file")
	}
	return recipesIndex, err
}

func (cmd *SyncCMD) UpsertRecipe(ctx context.Context, cli *CLI, c *paprika.Client, ref paprika.RecipeItem, log zerolog.Logger) (bool, error) {
	recipePath := pathToRecipeJSONFile(cli.DataDir, ref.UID)
	log = log.With().Str("recipe-file", recipePath).Logger()

	// Determine if recipe file should be created/updated/skipped
	var recipeFileAction string
	if doUpdate, exists := shouldSaveRecipe(recipePath, ref.Hash, log); !doUpdate {
		log.Debug().Msg("local recipe exists and does not require update")
		return false, nil
	} else if exists {
		log.Debug().Msg("local recipe exists and requires update")
		recipeFileAction = "update"
	} else {
		log.Debug().Msg("local recipe does not yet exist")
		recipeFileAction = "create"
	}
	log = log.With().Str("recipe-file-action", recipeFileAction).Logger()

	log.Debug().Msg("fetching recipe from API")
	recipe, err := c.Recipe(ctx, ref.UID)
	if err != nil {
		log.Err(err).Msg("failed to retrieve recipe from API")
		return false, err
	}

	if recipe.Hash != ref.Hash {
		// recipe may have been updated since retrieving the reference hash,
		// or the fetched recipe is stale if it matches the has on disk
		log = log.With().Str("recipe-fetched-hash", recipe.Hash).Logger()
		log.Warn().Msg("fetched recipe hash does not match reference hatch")
	}
	if recipe.UID != ref.UID {
		// this would be a major API issue
		err := fmt.Errorf("fetched recipe UID %q does not match requested UID %q", recipe.UID, ref.UID)
		log.Err(err).Str("recipe-fetched-uid", recipe.Hash).Msg("rejecting fetched recipe")
		return false, err
	}

	if err := saveAsJSON(recipe, recipePath); err != nil {
		log.Err(err).Msg("failed to save recipe file")
		return false, err
	}
	log.Info().Msg("saved recipe file")
	return true, nil
}

func shouldSaveRecipe(path, hash string, log zerolog.Logger) (update bool, exists bool) {
	f, err := os.Open(path)
	if err != nil {
		log.Debug().Msg("no extant recipe file")
		return true, false
	}

	var extantItem paprika.RecipeItem
	if err := json.NewDecoder(f).Decode(&extantItem); err != nil {
		log.Err(err).Msg("failed decoding extant recipe file JSON")
		return true, true
	}
	if extantItem.Hash == hash {
		log.Debug().Msg("extant recipe file matches latest recipe hash")
		return false, true
	}

	log.Debug().Str("recipe-extant-hash", extantItem.Hash).
		Msg("extant recipe file does not match latest recipe hash")
	return true, true
}

func saveAsJSON(val any, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(val); err != nil {
		return err
	}
	return nil
}

func (cmd *SyncCMD) PurgeUnreferencedRecipes(ctx context.Context, dataDir string, now time.Time, log zerolog.Logger) error {
	cutoff := now.Add(-cmd.PurgeAfter)
	log = log.With().
		Time("purge-cutoff", cutoff).
		Time("check-timestamp", now).
		Logger()
	nowStamp := now.Format(time.RFC3339Nano)

	var index []paprika.RecipeItem
	indexFile, err := os.Open(pathToRecipesIndexFile(dataDir))
	if err != nil {
		return err
	} else if err := json.NewDecoder(indexFile).Decode(&index); err != nil {
		return err
	}
	indexedUIDs := make(map[string]struct{}, len(index))
	for _, item := range index {
		indexedUIDs[item.UID] = struct{}{}
	}

	recipesDataRoot := pathToRecipesDir(dataDir)
	return filepath.WalkDir(recipesDataRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return err
		}

		// Skip all that is not a recipe or deletion marker file
		if d.IsDir() {
			return nil
		}
		currentFileName := d.Name()
		if currentFileName != filenameRecipeJSON && currentFileName != filenameRecipeDeleteMarker {
			return nil
		}

		dir := filepath.Dir(path)
		uid := filepath.Base(dir)
		log := log.With().
			Str("recipe-directory", dir).
			Str("recipe-uid", uid).
			Str("filename", currentFileName).
			Logger()

		// Check if recipe is present in index
		if _, exists := indexedUIDs[uid]; exists {
			if currentFileName == filenameRecipeDeleteMarker {
				if err := os.Remove(path); err != nil {
					log.Err(err).Msg("failed to delete stale deletion marker file for indexed recipe")
					return err
				}
				log.Debug().Msg("deleted stale deletion marker file for indexed recipe")
				// No need to continue inspecting this directory's contents
				return filepath.SkipDir
			}
			return nil
		}

		// Directory pertains to an unindexed recipe, likely because it was deleted from Paprika.
		// Do one of the following:
		// - Purge now if immediate purge is requested or a timestamp marker exists and is expired.
		// - If no timestamp marker exists, create one.
		// - If a timestamp marker already exists but has not expired, do nothing.
		doPurge := false
		if cmd.PurgeAfter == 0 {
			// Skip checking for timestamp marker and purge immediately
			doPurge = true
			log = log.With().Str("purge-reason", "immediate purge requested").Logger()
		} else if currentFileName == filenameRecipeDeleteMarker {
			// Note: Recipe has not been seen in index since marker was set.
			marker, err := readTimestampMarker(path, time.RFC3339Nano)
			if err != nil {
				log.Err(err).Msg("failed to read timestamp marker file")
				return err
			}
			log = log.With().Time("recipe-unindexed-since", marker).Logger()
			if marker.After(cutoff) {
				log.Debug().Msg("ignoring unindexed local recipe data because marker is more recent than cutoff")
				return filepath.SkipDir
			}
			doPurge = true
			log = log.With().Str("purge-reason", "recipe not seen in index since cutoff").Logger()
		}

		if doPurge {
			if err = os.RemoveAll(dir); err != nil {
				log.Err(err).Msg("failed to delete local data directory for unindexed recipe")
			}
			log.Info().Msg("deleted local data for unindexed recipe")
			return filepath.SkipDir
		}

		if currentFileName == filenameRecipeJSON {
			// Create marker file if one does not already exist
			f, err := os.OpenFile(pathToRecipeDeleteMarkerFile(dataDir, uid),
				os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
			if err != nil {
				if os.IsExist(err) {
					// Marker already exists
					return nil
				}
				log.Err(err).Msg("failed to create deletion marker file for unindexed recipe")
				return err
			}
			defer f.Close()
			if _, err := f.WriteString(nowStamp); err != nil {
				log.Err(err).Msg("failed to write deletion marker file for unindexed recipe")
				return err
			}
			log.Info().Msg("wrote new deletion marker file for unindexed recipe")
			return filepath.SkipDir
		}

		return nil
	})
}

// readTimestampMarker reads the file at path and returns the decoded timestamp marker.
func readTimestampMarker(path, layout string) (t time.Time, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}

	buf := make([]byte, len(layout))
	n, err := f.Read(buf)
	if err != nil {
		return
	}

	return time.Parse(layout, string(buf[:n]))
}

// PruneFilelessSubtrees removes subdirectories under the given root directory tree
// that themselves consist of only directories, recursively.
// root itself is never removed.
// Calls to os.RemoveAll() are optimized to occur at the top-most possible level,
// in order to minimize filesystem writes.
func PruneFilelessSubtrees(ctx context.Context, root string) error {
	// Recursive directory traverse-and-prune function
	var pruneDir func(dir string) (fileless bool, err error)
	pruneDir = func(dir string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, fmt.Errorf("read dir %q: %w", dir, err)
		}

		hasOnlyDirs := true
		var emptyChildren []string
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if !e.IsDir() {
				hasOnlyDirs = false
				continue
			}

			childPath := filepath.Join(dir, e.Name())

			childHasOnlyDirs, err := pruneDir(childPath)
			if err != nil {
				return false, err
			}

			if childHasOnlyDirs {
				emptyChildren = append(emptyChildren, childPath)
			} else {
				hasOnlyDirs = false
			}
		}

		if !hasOnlyDirs {
			// This dir cannot be entirely removed, so remove any fileless children now.
			for _, p := range emptyChildren {
				if err := ctx.Err(); err != nil {
					return false, err
				}
				if err := os.RemoveAll(p); err != nil {
					return false, fmt.Errorf("remove %q: %w", p, err)
				}
			}
		}

		return hasOnlyDirs, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read root %q: %w", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		childPath := filepath.Join(root, e.Name())

		childHasOnlyDirs, err := pruneDir(childPath)
		if err != nil {
			return err
		}

		if childHasOnlyDirs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := os.RemoveAll(childPath); err != nil {
				return fmt.Errorf("remove %q: %w", childPath, err)
			}
		}
	}

	return nil
}
