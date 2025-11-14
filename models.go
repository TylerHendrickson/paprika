package paprika

import "encoding/json"

type RecipeItem struct {
	Hash string `json:"hash,omitempty"`
	UID  string `json:"uid,omitempty"`
}

type Result struct {
	Result *json.RawMessage
}

type Status struct {
	Recipes    int `json:"recipes,omitempty"`
	Pantry     int `json:"pantry,omitempty"`
	Meals      int `json:"meals,omitempty"`
	Menus      int `json:"menus,omitempty"`
	Groceries  int `json:"groceries,omitempty"`
	Bookmarks  int `json:"bookmarks,omitempty"`
	Menuitems  int `json:"menuitems,omitempty"`
	Categories int `json:"categories,omitempty"`
}

type Bookmark struct {
	URL       string `json:"url,omitempty"`
	Title     string `json:"title,omitempty"`
	UID       string `json:"uid,omitempty"`
	OrderFlag int    `json:"order_flag,omitempty"`
}

type Category struct {
	Name      string `json:"name,omitempty"`
	UID       string `json:"uid,omitempty"`
	ParentUID string `json:"parent_uid,omitempty"`
	OrderFlag int    `json:"order_flag,omitempty"`
}

type GroceryItem struct {
	Aisle      string `json:"aisle,omitempty"`
	UID        string `json:"uid,omitempty"`
	OrderFlag  int    `json:"order_flag,omitempty"`
	Recipe     string `json:"recipe,omitempty"`
	Name       string `json:"name,omitempty"`
	Purchased  bool   `json:"purchased,omitempty"`
	RecipeUID  string `json:"recipe_uid,omitempty"`
	Ingredient string `json:"ingredient,omitempty"`
}

type Meal struct {
	UID       string `json:"uid,omitempty"`
	OrderFlag int    `json:"order_flag,omitempty"`
	RecipeUID string `json:"recipe_uid,omitempty"`
	Date      string `json:"date,omitempty"`
	Type      int    `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
}

type MenuItem struct {
	Name      string `json:"name,omitempty"`
	RecipeUID string `json:"recipe_uid,omitempty"`
	UID       string `json:"uid,omitempty"`
	MenuUID   string `json:"menu_uid,omitempty"`
	OrderFlag int    `json:"order_flag,omitempty"`
}

type PantryItem struct {
	Aisle      string `json:"aisle,omitempty"`
	UID        string `json:"uid,omitempty"`
	Ingredient string `json:"ingredient,omitempty"`
}

type Recipe struct {
	Rating          int      `json:"rating,omitempty"`
	PhotoHash       string   `json:"photo_hash,omitempty"`
	OnFavorites     bool     `json:"on_favorites,omitempty"`
	Photo           string   `json:"photo,omitempty"`
	UID             string   `json:"uid,omitempty"`
	Scale           string   `json:"scale,omitempty"`
	Ingredients     string   `json:"ingredients,omitempty"`
	Source          string   `json:"source,omitempty"`
	Hash            string   `json:"hash,omitempty"`
	SourceURL       string   `json:"source_url,omitempty"`
	Difficulty      string   `json:"difficulty,omitempty"`
	Categories      []string `json:"categories,omitempty"`
	PhotoURL        string   `json:"photo_url,omitempty"`
	CookTime        string   `json:"cook_time,omitempty"`
	Name            string   `json:"name,omitempty"`
	Created         string   `json:"created,omitempty"`
	Notes           string   `json:"notes,omitempty"`
	ImageURL        string   `json:"image_url,omitempty"`
	PrepTime        string   `json:"prep_time,omitempty"`
	Servings        string   `json:"servings,omitempty"`
	NutritionalInfo string   `json:"nutritional_info,omitempty"`
	Directions      string   `json:"directions,omitempty"`
}
