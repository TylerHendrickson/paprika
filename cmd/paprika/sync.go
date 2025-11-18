package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

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
	IncludeRecipes    bool       `help:"Whether to (not) include recipes." negatable:"" default:"true" env:"PAPRIKA_SYNC_RECIPES"`
	IncludeCategories bool       `help:"Whether to (not) include categories." negatable:"" default:"true" env:"PAPRIKA_SYNC_CATEGORIES"`
	NumWorkers        NumWorkers `help:"Number of workers to download recipes in parallel." default:"10" env:"PAPRIKA_SYNC_WORKERS"`
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
		recipesQueue := make(chan paprika.RecipeItem, cmd.NumWorkers)
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

		for i := range cmd.NumWorkers {
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
						updated, err := cmd.UpsertRecipe(ctx, cli, pc, ref, log)
						if err != nil {
							exitWithErrors.Store(true)
							log.Err(err).Msg("worker task failed for recipe item in queue")
						}
						if updated {
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

	path := filepath.Join(cli.DataDir, "categories-index.json")
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

	path := filepath.Join(cli.DataDir, "recipes-index.json")
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
	path := filepath.Join(cli.DataDir, "recipes", ref.UID[:2], ref.UID[:3], ref.UID, "recipe.json")
	log = log.With().Str("recipe-file", path).Logger()

	recipeFileAction := "create"
	if doUpdate, exists := shouldSaveRecipe(path, ref.Hash, log); !doUpdate {
		log.Debug().Msg("not saving recipe")
		return false, nil
	} else if exists {
		recipeFileAction = "update"
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

	if err := saveAsJSON(recipe, path); err != nil {
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
