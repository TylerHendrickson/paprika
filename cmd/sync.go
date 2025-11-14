package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	IncludeCategories bool       `help:"Whether to (not) include categories." negatable:"" default:"true" env:"PAPRIKA_SYNC_CATEGORIES"`
	IncludeRecipes    bool       `help:"Whether to (not) include recipes." negatable:"" default:"true" env:"PAPRIKA_SYNC_RECIPES"`
	NumWorkers        NumWorkers `help:"Number of workers to download recipes in parallel." default:"10" env:"PAPRIKA_SYNC_WORKERS"`
}

func (cmd *SyncCMD) Run(ctx context.Context, cli *CLI, pc *paprika.Client, log zerolog.Logger) error {
	wg := sync.WaitGroup{}
	if cmd.IncludeCategories {
		wg.Go(func() { cmd.SaveCategoriesIndex(ctx, cli, pc, log) })
	}

	if !cmd.IncludeRecipes {
		wg.Wait()
		return nil
	}

	recipeIndexItems, err := cmd.SaveRecipesIndex(ctx, cli, pc, log)
	if err != nil {
		log.Err(err).Msg("cannot sync recipes due to error")
		wg.Wait()
		return reportedErr{err}
	}
	recipesQueue := make(chan paprika.RecipeItem, cmd.NumWorkers)
	for i := range cmd.NumWorkers {
		wg.Go(func() {
			log := log.With().Int("worker-id", int(i)+1).Int("total-workers", int(cmd.NumWorkers)).Logger()
			for {
				select {
				case <-ctx.Done():
					log.Warn().Err(ctx.Err()).Msg("shutdown requested while waiting for work")
					return

				case ref, ok := <-recipesQueue:
					if !ok {
						log.Debug().Msg("worker shutting down because the queue is closed and empty")
						return
					}
					itemLogger := log.With().Str("recipe-uid", ref.UID).Str("recipe-indexed-hash", ref.Hash).Logger()
					itemLogger.Debug().Msg("starting work for recipe item")
					if err := cmd.UpsertRecipe(ctx, cli, pc, ref, itemLogger); err != nil {
						itemLogger.Err(err).Msg("worker task failed for recipe item in queue")
					}
					itemLogger.Debug().Msg("completed work for recipe item")
				}
			}
		})
	}

	for _, item := range recipeIndexItems {
		log.Trace().Str("recipe-uid", item.UID).Str("recipe-indexed-hash", item.Hash).Msg("adding recipe item to work queue")
		recipesQueue <- item
	}
	close(recipesQueue)
	log.Debug().Msg("all recipe jobs queued; waiting to complete")
	wg.Wait()
	log.Info().Msg("finished processing recipes")
	return nil
}

func (cmd *SyncCMD) SaveCategoriesIndex(ctx context.Context, cli *CLI, c *paprika.Client, log zerolog.Logger) error {
	categories, err := c.Categories(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get categories")
		return err
	}

	path := filepath.Join(cli.DataDir, "categories-index.json")
	log = log.With().Str("path", path).Logger()
	if err := saveAsJSON(categories, path); err != nil {
		log.Err(err).Msg("save to create Paprika categories index file")
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

func (cmd *SyncCMD) UpsertRecipe(ctx context.Context, cli *CLI, c *paprika.Client, ref paprika.RecipeItem, log zerolog.Logger) error {
	path := filepath.Join(cli.DataDir, "recipes", ref.UID[:2], ref.UID[:3], ref.UID, "recipe.json")
	log = log.With().Str("recipe-file", path).Logger()

	recipeFileAction := "create"
	if doUpdate, exists := shouldSaveRecipe(path, ref.Hash, log); !doUpdate {
		log.Debug().Msg("not saving recipe")
		return nil
	} else if exists {
		recipeFileAction = "update"
	}
	log = log.With().Str("recipe-file-action", recipeFileAction).Logger()

	log.Debug().Msg("fetching recipe from API")
	recipe, err := c.Recipe(ctx, ref.UID)
	if err != nil {
		log.Err(err).Msg("failed to retrieve recipe from API")
	}
	if recipe.Hash != ref.Hash {
		// recipe may have been updated since retrieving the reference hash,
		// or the fetched recipe is stale if it matches the has on disk
		log = log.With().Str("recipe-fetched-hash", recipe.Hash).Logger()
		log.Warn().Msg("fetched recipe hash does not match reference hatch")
	} else if recipe.UID != ref.UID {
		// this would be a major API issue
		log = log.With().Str("recipe-fetched-uid", recipe.Hash).Logger()
		err := fmt.Errorf("fetched recipe UID %q does not match reference UID %q", recipe.UID, ref.UID)
		log.Err(err).Msg("rejecting updated recipe because the fetched UID does not match the requested UID")
	}

	if err := saveAsJSON(recipe, path); err != nil {
		log.Err(err).Msg("failed to save recipe file")
		return err
	}
	log.Info().Msg("saved recipe file")
	return nil
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

	log.Debug().Str("recipe-extant-hash", extantItem.Hash).Msg("extant recipe file does not match latest recipe hash")
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
