package main

import (
	"fmt"
	"github.com/go-redis/redis/v7"
	"log"
	"time"
)

type recipe struct {
	id          int64
	title       string
	difficulty  string
	prepPeriod  time.Duration
	method      string
	categories  []string
	ingredients []string
	images      []string
}

// save used for Create or Update
func (r *recipe) save(c *redis.Client) error {
	var save bool

	if r.id == 0 {
		save = true
		id, err := c.Incr("recipe_id").Result()
		if err != nil {
			return err
		}
		r.id = id
	}

	_, err := c.TxPipelined(func(pipe redis.Pipeliner) error {
		if save {
			if err := pipe.RPush("recipes", r.id).Err(); err != nil {
				return err
			}
		}
		pipe.HMSet(fmt.Sprintf("recipe:%d", r.id),
			"id", r.id,
			"title", r.title,
			"difficulty", r.difficulty,
			"prep_period", r.prepPeriod.String(),
			"method", r.method,
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

		saveList(r.id, "categories", r.categories, c, pipe)
		saveList(r.id, "ingredients", r.ingredients, c, pipe)
		saveList(r.id, "images", r.images, c, pipe)

		return nil
	})

	return err
}

func (r *recipe) load(id int64, c *redis.Client) error {
	return nil
}

func list(page int, c *redis.Client) ([]recipe, error) {

	return nil, nil
}

func main() {

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

	r := &recipe{
		title:       "PanCake",
		difficulty:  "easy",
		prepPeriod:  10 * time.Minute,
		method:      "",
		categories:  []string{"breakfast", "eastern"},
		ingredients: []string{"eggs", "corn"},
		images:      []string{"url1", "url2"},
	}

	for i := 0; i < 10; i++ {
		if err := r.save(client); err != nil {
			log.Fatal(err)
		}
	}
}
