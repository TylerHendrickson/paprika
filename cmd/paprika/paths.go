package main

import "path/filepath"

const (
	filenameRecipeJSON         string = "recipe.json"
	filenameRecipeDeleteMarker string = ".delete-marker"
	filenameRecipesIndex       string = "recipes-index.json"
	filenameCategoriesIndex    string = "categories-index.json"
)

func pathToRecipeDir(basePath, uid string) string {
	return filepath.Join(pathToRecipesDir(basePath), uid[:2], uid[:3], uid)
}

func pathToRecipeJSONFile(basePath, uid string) string {
	return filepath.Join(pathToRecipeDir(basePath, uid), filenameRecipeJSON)
}

func pathToRecipeDeleteMarkerFile(basePath, uid string) string {
	return filepath.Join(pathToRecipeDir(basePath, uid), filenameRecipeDeleteMarker)
}

func pathToRecipesDir(basePath string) string {
	return filepath.Join(basePath, "recipes")
}

func pathToRecipesIndexFile(basePath string) string {
	return filepath.Join(basePath, filenameRecipesIndex)
}

func pathToCategoriesIndexFile(basePath string) string {
	return filepath.Join(basePath, filenameCategoriesIndex)
}
