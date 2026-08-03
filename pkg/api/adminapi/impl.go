package adminapi

import (
	"context"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/internal/values"

	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
)

// adminAPIService is the Admin API's translation layer: it parses the wire,
// delegates to a domain service, and renders the answer.
//
// It holds services rather than repositories, and that is the point of this file
// rather than an incidental refactor. Authorization is declared at the service seam
// (internal/domains, internal/templates, internal/apikeys), so the wiring below is
// what decides whether an operation is checked at all. A method reaching past a
// service to a repository would compile, work, and be unguarded.
type adminAPIService struct {
	domains   *domains.Service
	templates *templates.Service
	apiKeys   *apikeys.Service
}

func (s *adminAPIService) GetDomains(ctx context.Context, in *pb.GetDomainsReq) (*pb.GetDomainsResponse, error) {
	all, err := s.domains.GetDomains(ctx)
	if err != nil {
		return nil, err
	}

	res := pb.GetDomainsResponse{}
	for _, d := range all {
		res.Domains = append(res.Domains, d.Pb())
	}
	return &res, nil
}

// The request field is a bare string, so this handler is where an FQDN enters
// the system: it is parsed here and the error is returned to the caller, rather
// than being carried any deeper as a string.
func (s *adminAPIService) GetDomain(ctx context.Context, in *pb.GetDomainReq) (*pb.GetDomainRes, error) {
	name, err := values.Parse(in.Domain)
	if err != nil {
		return nil, err
	}

	d, err := s.domains.GetDomain(ctx, name)
	if err != nil {
		return nil, err
	}

	return &pb.GetDomainRes{
		Domain: d.Pb(),
	}, nil
}

func (s *adminAPIService) CreateDomain(ctx context.Context, in *pb.CreateDomainRequest) (*pb.Domain, error) {
	name, err := values.Parse(in.Domain)
	if err != nil {
		return nil, err
	}

	d, err := s.domains.CreateDomain(ctx, name)
	if err != nil {
		return nil, err
	}
	return d.Pb(), nil
}

func (s *adminAPIService) SetTrackingPolicy(ctx context.Context, in *pb.SetTrackingPolicyReq) (*pb.SetTrackingPolicyRes, error) {
	name, err := values.Parse(in.Domain)
	if err != nil {
		return nil, err
	}

	policy, err := trackingpb.ToPolicy(in.Tracking)
	if err != nil {
		return nil, err
	}

	d, err := s.domains.SetTrackingPolicy(ctx, name, policy)
	if err != nil {
		return nil, err
	}

	return &pb.SetTrackingPolicyRes{Domain: d.Pb()}, nil
}
