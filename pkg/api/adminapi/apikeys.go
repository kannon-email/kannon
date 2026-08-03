package adminapi

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/values"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *adminAPIService) CreateAPIKey(ctx context.Context, in *pb.CreateAPIKeyRequest) (*pb.CreateAPIKeyResponse, error) {
	// The request carries the domain as a bare string, so this is where it
	// becomes a canonical domain name.
	domain, err := values.Parse(in.Domain)
	if err != nil {
		return nil, err
	}

	var expiresAt *time.Time
	if in.ExpiresAt != nil {
		t := in.ExpiresAt.AsTime()
		expiresAt = &t
	}

	result, err := s.apiKeys.CreateKey(ctx, domain, in.Name, expiresAt)
	if err != nil {
		return nil, err
	}

	// Return FULL key (only time it's shown)
	return &pb.CreateAPIKeyResponse{
		ApiKey: apiKeyToProto(result.Key),
		Key:    result.PlaintextKey,
	}, nil
}

// ListAPIKeys lists all API keys for a domain (masked)
func (s *adminAPIService) ListAPIKeys(ctx context.Context, in *pb.ListAPIKeysRequest) (*pb.ListAPIKeysResponse, error) {
	domain, err := values.Parse(in.Domain)
	if err != nil {
		return nil, err
	}

	page := apikeys.Pagination{
		Limit:  100,
		Offset: 0,
	}
	if in.Limit > 0 {
		page.Limit = int(in.Limit)
	}
	if in.Offset > 0 {
		page.Offset = int(in.Offset)
	}

	keys, total, err := s.apiKeys.ListKeys(ctx, domain, in.OnlyActive, page)
	if err != nil {
		return nil, err
	}

	protoKeys := make([]*pb.APIKey, len(keys))
	for i, key := range keys {
		protoKeys[i] = apiKeyToProto(key)
	}

	return &pb.ListAPIKeysResponse{
		ApiKeys: protoKeys,
		Total:   int32(total),
	}, nil
}

// GetAPIKey gets a single API key by ID (masked)
func (s *adminAPIService) GetAPIKey(ctx context.Context, in *pb.GetAPIKeyRequest) (*pb.GetAPIKeyResponse, error) {
	ref, err := apikeys.ParseKeyRef(in.Domain, in.Id)
	if err != nil {
		return nil, err
	}

	key, err := s.apiKeys.GetKey(ctx, ref)
	if err != nil {
		return nil, err
	}

	return &pb.GetAPIKeyResponse{
		ApiKey: apiKeyToProto(key),
	}, nil
}

// DeactivateAPIKey deactivates an API key (masked)
func (s *adminAPIService) DeactivateAPIKey(ctx context.Context, in *pb.DeactivateAPIKeyRequest) (*pb.DeactivateAPIKeyResponse, error) {
	ref, err := apikeys.ParseKeyRef(in.Domain, in.Id)
	if err != nil {
		return nil, err
	}

	key, err := s.apiKeys.DeactivateKey(ctx, ref)
	if err != nil {
		return nil, err
	}

	return &pb.DeactivateAPIKeyResponse{
		ApiKey: apiKeyToProto(key),
	}, nil
}

func apiKeyToProto(key *apikeys.APIKey) *pb.APIKey {
	apiKey := &pb.APIKey{
		Id:       key.ID().String(),
		Name:     key.Name(),
		Domain:   key.Domain(),
		IsActive: key.IsActiveStatus(),
		Key:      key.MaskedKey(),
	}

	apiKey.CreatedAt = timestamppb.New(key.CreatedAt())
	if key.ExpiresAt() != nil {
		apiKey.ExpiresAt = timestamppb.New(*key.ExpiresAt())
	}
	if key.DeactivatedAt() != nil {
		apiKey.DeactivatedAt = timestamppb.New(*key.DeactivatedAt())
	}

	return apiKey
}
