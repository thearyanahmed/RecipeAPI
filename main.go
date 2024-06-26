package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/go-redis/redis/v7"
	"github.com/gorilla/mux"
)

var rdb *redis.Client

func main() {
	host := os.Getenv("REDIS_HOST")
	pass := os.Getenv("REDIS_PASSWORD")
	insecure, err := strconv.ParseBool(os.Getenv("REDIS_INSECURE_SKIP_VERIFY"))

	if err != nil {
		fmt.Println("Error parsing boolean:", err)
		insecure = false
		fmt.Println("Defaulting to false")
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     host,
		Password: pass,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: insecure, // Only for self-signed certificates
		},
	})

	r := mux.NewRouter()

	r.Path("/recipe").Methods("POST").HandlerFunc(CreateHandler)
	r.Path("/recipe/{id}").Methods("PUT").HandlerFunc(UpdateHandler)
	r.Path("/recipe/{id}").Methods("GET").HandlerFunc(GetHandler)
	r.Path("/recipes").Methods("GET").HandlerFunc(ListHandler)

	fmt.Println("will be starting using ", host, pass)
	log.Fatal(http.ListenAndServe(":8080", r))
}

func CreateHandler(w http.ResponseWriter, r *http.Request) {

	var recipe recipe
	err := json.NewDecoder(r.Body).Decode(&recipe)
	if err != nil {
		handleError(w, err)
		return
	}

	err = recipe.save(rdb)
	if err != nil {
		handleError(w, err)
		return
	}
}

func UpdateHandler(w http.ResponseWriter, r *http.Request) {

	id := mux.Vars(r)["id"]
	idInt, err := strconv.Atoi(id)
	if err != nil {
		handleError(w, err)
		return
	}

	var recipe recipe
	err = json.NewDecoder(r.Body).Decode(&recipe)
	if err != nil {
		handleError(w, err)
		return
	}

	recipe.ID = int64(idInt)
	err = recipe.save(rdb)
	if err != nil {
		handleError(w, err)
		return
	}
}

func GetHandler(w http.ResponseWriter, r *http.Request) {

	id := mux.Vars(r)["id"]
	idInt, err := strconv.Atoi(id)
	if err != nil {
		handleError(w, err)
		return
	}

	var recipe = &recipe{}

	err = recipe.load(int64(idInt), rdb)

	fmt.Printf("err %v\n", err)

	if err != nil {
		handleError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(recipe)
	if err != nil {
		handleError(w, err)
		return
	}
}

func ListHandler(w http.ResponseWriter, r *http.Request) {

	pageParam, ok := r.URL.Query()["page"]
	if !ok {
		handleError(w, errors.New("missing page parameter"))
		return
	}

	page, err := strconv.Atoi(pageParam[0])
	if err != nil {
		handleError(w, err)
		return
	}

	l, err := list(page, rdb)
	if err != nil {
		handleError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(l)
	if err != nil {
		handleError(w, err)
		return
	}
}

func handleError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(err.Error()))
}

type recipe struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Difficulty  string   `json:"difficulty,omitempty"`
	PrepPeriod  string   `json:"prep_period,omitempty"`
	Method      string   `json:"method,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	Ingredients []string `json:"ingredients,omitempty"`
	Images      []string `json:"images,omitempty"`
}

// save used for Create or Update
func (r *recipe) save(c *redis.Client) error {
	var save bool

	if r.ID == 0 {
		save = true
		id, err := c.Incr("recipe_id").Result()
		if err != nil {
			return err
		}
		r.ID = id
	}

	_, err := c.TxPipelined(func(pipe redis.Pipeliner) error {
		if save {
			if err := pipe.RPush("recipes", r.ID).Err(); err != nil {
				return err
			}
		}
		pipe.HMSet(fmt.Sprintf("recipe:%d", r.ID),
			"id", r.ID,
			"title", r.Title,
			"difficulty", r.Difficulty,
			"prep_period", r.PrepPeriod,
			"method", r.Method,
		)

		saveList := func(recipeId int64, name string, values []string, c *redis.Client, pipe redis.Pipeliner) {
			if values == nil {
				return
			}
			key := fmt.Sprintf("recipe:%d:%s", recipeId, name)
			if c.Exists(key).Val() == 1 {
				return
			}
			pipe.RPush(key, values)
		}

		saveList(r.ID, "categories", r.Categories, c, pipe)
		saveList(r.ID, "ingredients", r.Ingredients, c, pipe)
		saveList(r.ID, "images", r.Images, c, pipe)

		return nil
	})

	return err
}

func (r *recipe) load(id int64, c *redis.Client) error {
	if id <= 0 {
		return errors.New("invalid id")
	}

	r.ID = id

	var hgetAllCmd *redis.StringStringMapCmd
	var listCmds [3]*redis.StringSliceCmd

	_, err := c.Pipelined(func(pipe redis.Pipeliner) error {
		hgetAllCmd = pipe.HGetAll(fmt.Sprintf("recipe:%d", r.ID))

		for i, l := range []string{"categories", "ingredients", "images"} {
			listCmds[i] = pipe.LRange(fmt.Sprintf("recipe:%d:%s", r.ID, l), 0, -1)
		}
		return nil
	})
	if err != nil {
		return err
	}

	result, err := hgetAllCmd.Result()
	if err != nil {
		return err
	}
	r.Title = result["title"]
	r.Difficulty = result["difficulty"]
	r.PrepPeriod = result["prep_period"]
	r.Method = result["method"]

	loadList := func(list ...*[]string) error {
		for i := range list {
			strings, err := listCmds[i].Result()
			if err != nil {
				return err
			}
			*list[i] = strings
		}
		return nil
	}
	err = loadList(&r.Categories, &r.Ingredients, &r.Images)
	if err != nil {
		return err
	}

	return nil
}

func list(page int, c *redis.Client) ([]recipe, error) {
	if page <= 0 {
		return nil, errors.New("invalid page")
	}

	const pageSize int64 = 20
	from, to := (int64(page)-1)*pageSize, int64(page)*pageSize-1

	recipeIds, err := c.LRange("recipes", from, to).Result()
	if err != nil {
		return nil, err
	}

	var cmds []*redis.SliceCmd
	_, err = c.Pipelined(func(pipe redis.Pipeliner) error {
		for _, recipeId := range recipeIds {
			cmds = append(cmds, pipe.HMGet(fmt.Sprintf("recipe:%s", recipeId), "id", "title"))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var titles []recipe
	for _, c := range cmds {
		id, _ := strconv.Atoi(c.Val()[0].(string))
		titles = append(titles, recipe{
			ID:    int64(id),
			Title: c.Val()[1].(string),
		})
	}

	return titles, nil
}
