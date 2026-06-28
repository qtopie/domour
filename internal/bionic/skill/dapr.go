package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	daprClient "github.com/dapr/go-sdk/client"
)

type DaprRegistry struct {
	storeName string
	client    daprClient.Client
}

func NewDaprRegistry(storeName string) *DaprRegistry {
	if storeName == "" {
		storeName = "statestore"
	}
	return &DaprRegistry{
		storeName: storeName,
	}
}

func (r *DaprRegistry) Initialize(ctx context.Context) error {
	if r.client != nil {
		return nil
	}

	port := os.Getenv("DAPR_GRPC_PORT")
	if port == "" {
		port = "50001"
	}

	dc, err := daprClient.NewClientWithPort(port)
	if err != nil {
		return fmt.Errorf("create dapr client for skill registry: %w", err)
	}

	r.client = dc
	return nil
}

func (r *DaprRegistry) Register(ctx context.Context, s *Skill) error {
	if err := r.Initialize(ctx); err != nil {
		return err
	}
	if s == nil || s.ID == "" {
		return fmt.Errorf("invalid skill specification: ID is required")
	}

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("serialize skill: %w", err)
	}

	// Save skill details
	err = r.client.SaveState(ctx, r.storeName, "skill:data:"+s.ID, data, nil)
	if err != nil {
		return fmt.Errorf("save skill state: %w", err)
	}

	// Update index
	_ = r.updateIndex(ctx, s.ID, true)

	return nil
}

func (r *DaprRegistry) Get(ctx context.Context, id string) (*Skill, error) {
	if err := r.Initialize(ctx); err != nil {
		return nil, err
	}

	item, err := r.client.GetState(ctx, r.storeName, "skill:data:"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("get skill state: %w", err)
	}
	if len(item.Value) == 0 {
		return nil, fmt.Errorf("skill %s not found in dapr registry", id)
	}

	var s Skill
	if err := json.Unmarshal(item.Value, &s); err != nil {
		return nil, fmt.Errorf("deserialize skill: %w", err)
	}

	return &s, nil
}

func (r *DaprRegistry) List(ctx context.Context) ([]*Skill, error) {
	if err := r.Initialize(ctx); err != nil {
		return nil, err
	}

	index, err := r.getIndex(ctx)
	if err != nil {
		return nil, err
	}

	var list []*Skill
	for _, id := range index {
		s, err := r.Get(ctx, id)
		if err == nil {
			list = append(list, s)
		}
	}

	return list, nil
}

func (r *DaprRegistry) Delete(ctx context.Context, id string) error {
	if err := r.Initialize(ctx); err != nil {
		return err
	}

	err := r.client.DeleteState(ctx, r.storeName, "skill:data:"+id, nil)
	if err != nil {
		return fmt.Errorf("delete skill state: %w", err)
	}

	_ = r.updateIndex(ctx, id, false)
	return nil
}

func (r *DaprRegistry) getIndex(ctx context.Context) ([]string, error) {
	item, err := r.client.GetState(ctx, r.storeName, "skills:index", nil)
	if err != nil {
		return nil, fmt.Errorf("get skills index: %w", err)
	}
	if len(item.Value) == 0 {
		return nil, nil
	}

	var index []string
	if err := json.Unmarshal(item.Value, &index); err != nil {
		return nil, fmt.Errorf("deserialize skills index: %w", err)
	}
	return index, nil
}

func (r *DaprRegistry) updateIndex(ctx context.Context, id string, add bool) error {
	index, _ := r.getIndex(ctx)

	found := false
	var newIndex []string
	for _, existingID := range index {
		if existingID == id {
			found = true
			if add {
				newIndex = append(newIndex, existingID)
			}
		} else {
			newIndex = append(newIndex, existingID)
		}
	}

	if add && !found {
		newIndex = append(newIndex, id)
	}

	data, err := json.Marshal(newIndex)
	if err != nil {
		return err
	}

	return r.client.SaveState(ctx, r.storeName, "skills:index", data, nil)
}

func (r *DaprRegistry) Close() error {
	if r.client != nil {
		r.client.Close()
		r.client = nil
	}
	return nil
}
