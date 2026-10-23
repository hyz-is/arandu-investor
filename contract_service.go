package investor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/translation"
)

// MaxContractBytes is the largest signed file accepted. A scanned contract of
// a few pages is far below it; a file above it is a mistake, or worse.
const MaxContractBytes = 20 << 20

// contractTypes are the files a signed contract may be: the PDF, or a photo of
// the paper.
var contractTypes = map[string]string{
	"application/pdf": ".pdf",
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
}

// ContractRequest is the input contract for registering a contract or an
// amendment. The file comes beside it, as a reader, so the bytes never pass
// through a struct a log might print.
type ContractRequest struct {
	Number      string
	SignedOn    string
	StartsOn    string
	EndsOn      string
	ShareBps    int64
	BillingDay  int
	AllProducts bool
	Products    []string
	FileName    string
	FileType    string
}

// Validate reports the errors per field.
func (r ContractRequest) Validate() validation.Errors {
	e := validation.Errors{}
	validation.Required(e, "number", r.Number)
	validation.MaxLen(e, "number", r.Number, maxShortLen)
	signed, err := ParseDay(r.SignedOn)
	if err != nil || signed.IsZero() {
		e.Add("signed_on", message("day", dayLayout))
	}
	starts, err := ParseDay(r.StartsOn)
	if err != nil || starts.IsZero() {
		e.Add("starts_on", message("day", dayLayout))
	}
	ends, err := ParseDay(r.EndsOn)
	if err != nil {
		e.Add("ends_on", message("day_or_empty", dayLayout))
	} else if !ends.IsZero() && !starts.IsZero() && ends.Before(starts) {
		e.Add("ends_on", message("not_before_start"))
	}
	if r.ShareBps <= 0 || r.ShareBps > 10000 {
		e.Add("share_bps", message("share_range"))
	}
	// Up to the 28th, so every month has the day: a contract that closed on the
	// 31st would have no closing day in February.
	if r.BillingDay < 1 || r.BillingDay > 28 {
		e.Add("billing_day", message("billing_day"))
	}
	if !r.AllProducts && len(cleanProducts(r.Products)) == 0 {
		e.Add("products", message("contract_products"))
	}
	for _, product := range cleanProducts(r.Products) {
		if len(product) > maxIdentifierLen {
			e.Add("products", message("product_too_long", translation.Replace{"product": strconv.Quote(product), "max": maxIdentifierLen}))
		}
	}
	validation.Required(e, "file_name", r.FileName)
	validation.MaxLen(e, "file_name", r.FileName, maxNameLen)
	if _, ok := contractTypes[r.FileType]; !ok {
		e.Add("file", message("file_type"))
	}
	return e
}

var _ validation.Validatable = ContractRequest{}

// cleanProducts trims, drops empties and repeats, keeping the order given.
func cleanProducts(products []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(products))
	for _, p := range products {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// readContractFile reads the signed file, refusing one past the limit, and
// returns it with its digest.
func readContractFile(file io.Reader) ([]byte, string, error) {
	if file == nil {
		return nil, "", validation.Errors{"file": {message("required")}}
	}
	body, err := io.ReadAll(io.LimitReader(file, MaxContractBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 {
		return nil, "", validation.Errors{"file": {message("file_empty")}}
	}
	if len(body) > MaxContractBytes {
		return nil, "", validation.Errors{"file": {message("file_too_large", translation.Replace{"mb": MaxContractBytes >> 20})}}
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}

// CreateContract registers the original version of a contract for an
// investor, with the signed file.
func (s *InvestorService) CreateContract(ctx context.Context, actor security.Subject, investorID string, in ContractRequest, file io.Reader) (*Contract, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}
	owner, err := Investors(s.db).WhereKey(investorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, *owner); err != nil {
		return nil, err
	}
	contract, err := s.writeContract(ctx, actor, g, owner, nil, in, file)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventContractCreated, "investor_contract", contract.ID, map[string]any{"investor_id": owner.ID, "number": contract.Number, "share_bps": contract.ShareBps}); err != nil {
		return nil, err
	}
	return contract, nil
}

// AmendContract registers an amendment: a new version of the contract named,
// with its own terms and file. The version in force becomes superseded in the
// same transaction, so there is never a moment with two versions in force or
// with none.
func (s *InvestorService) AmendContract(ctx context.Context, actor security.Subject, contractID string, in ContractRequest, file io.Reader) (*Contract, error) {
	if errs := in.Validate(); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, Investor{})
	if err != nil {
		return nil, err
	}
	named, err := Contracts(s.db).WhereKey(contractID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if named == nil {
		return nil, ErrNotFound
	}
	owner, err := Investors(s.db).WhereKey(named.InvestorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, InvestorsManage, *owner); err != nil {
		return nil, err
	}
	// The version in force of the same contract, whichever version was named:
	// an amendment always follows the one in force.
	current, err := Contracts(s.db).Where("root_id", "=", named.RootID).
		Where("status", "=", string(ContractActive)).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, validation.Errors{"contract": {message("no_version_to_amend")}}
	}
	contract, err := s.writeContract(ctx, actor, g, owner, current, in, file)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, EventContractAmended, "investor_contract", contract.ID, map[string]any{"investor_id": owner.ID, "supersedes": current.ID, "version": contract.Version, "share_bps": contract.ShareBps}); err != nil {
		return nil, err
	}
	return contract, nil
}

// writeContract stores the file and the rows, after the caller authorized and
// loaded the investor. current is the version an amendment replaces, nil for
// an original.
func (s *InvestorService) writeContract(ctx context.Context, actor security.Subject, g security.Grant, owner *Investor, current *Contract, in ContractRequest, file io.Reader) (*Contract, error) {
	if s.files == nil {
		return nil, fmt.Errorf("investor: no Disk was configured for the signed contracts (Config.Files)")
	}
	body, digest, err := readContractFile(file)
	if err != nil {
		return nil, err
	}
	id, err := data.NewID()
	if err != nil {
		return nil, err
	}
	version, rootID := 1, id
	if current != nil {
		version, rootID = current.Version+1, current.RootID
	}
	signed, _ := ParseDay(in.SignedOn)
	starts, _ := ParseDay(in.StartsOn)
	ends, _ := ParseDay(in.EndsOn)

	// The file goes first, under a key nobody can guess or collide with. If the
	// rows that follow fail, the file is an orphan nobody can reach; the other
	// order would leave a contract that points at nothing.
	key := path.Join("investor", "contracts", id, digest+contractTypes[in.FileType])
	if err := s.files.Put(ctx, g, key, bytes.NewReader(body), in.FileType); err != nil {
		return nil, err
	}

	record, err := Contracts(s.db).New()
	if err != nil {
		return nil, err
	}
	record.ID = id
	record.TenantID = data.Tenant(g)
	record.InvestorID = owner.ID
	record.Number = strings.TrimSpace(in.Number)
	record.Version = version
	record.RootID = rootID
	record.SignedOn, record.StartsOn, record.EndsOn = signed, starts, ends
	record.ShareBps = in.ShareBps
	record.BillingDay = in.BillingDay
	record.AllProducts = Flag(in.AllProducts)
	record.Status = ContractActive
	record.FileKey = key
	record.FileName = strings.TrimSpace(in.FileName)
	record.FileType = in.FileType
	record.FileSize = int64(len(body))
	record.FileSHA256 = digest
	record.CreatedBy = actor.ID
	record.CreatedAt = s.now()
	record.UpdatedAt = record.CreatedAt

	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if current != nil {
			if err := current.Supersede(); err != nil {
				return err
			}
			if _, err := current.Save(ctx, g); err != nil {
				return err
			}
		}
		if _, err := record.Save(ctx, g); err != nil {
			return err
		}
		if record.AllProducts {
			return nil
		}
		for _, product := range cleanProducts(in.Products) {
			link, err := ContractProducts(s.db).New()
			if err != nil {
				return err
			}
			if link.ID, err = data.NewID(); err != nil {
				return err
			}
			link.TenantID = data.Tenant(g)
			link.ContractID = record.ID
			link.ProductID = product
			link.CreatedAt = record.CreatedAt
			link.UpdatedAt = record.CreatedAt
			if _, err := link.Save(ctx, g); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}

// InvestorContracts lists every version of every contract of one investor,
// newest first, with the products of each. The admin may ask about anybody;
// an investor only about themselves.
func (s *InvestorService) InvestorContracts(ctx context.Context, actor security.Subject, investorID string) ([]ContractResource, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ContractView, Investor{})
	if err != nil {
		return nil, err
	}
	owner, err := Investors(s.db).WhereKey(investorID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, ContractView, *owner); err != nil {
		return nil, err
	}
	contracts, err := Contracts(s.db).Where("investor_id", "=", owner.ID).
		OrderByDesc("created_at").OrderByDesc("id").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	out := make([]ContractResource, 0, len(contracts))
	for _, c := range contracts {
		var products []string
		if !c.AllProducts {
			links, err := ContractProducts(s.db).Where("contract_id", "=", c.ID).OrderBy("product_id").Get(ctx, g)
			if err != nil {
				return nil, err
			}
			for _, link := range links {
				products = append(products, link.ProductID)
			}
		}
		out = append(out, NewContractResource(*c, products))
	}
	return out, nil
}

// ContractFile opens the signed file of one contract version. The admin may
// open any; an investor only their own. The caller closes the body.
func (s *InvestorService) ContractFile(ctx context.Context, actor security.Subject, contractID string) (*Contract, filesystem.File, error) {
	g, err := security.Authorize(ctx, s.policy, actor, ContractView, Investor{})
	if err != nil {
		return nil, filesystem.File{}, err
	}
	record, err := Contracts(s.db).WhereKey(contractID).First(ctx, g)
	if err != nil {
		return nil, filesystem.File{}, err
	}
	if record == nil {
		return nil, filesystem.File{}, ErrNotFound
	}
	owner, err := Investors(s.db).WhereKey(record.InvestorID).First(ctx, g)
	if err != nil {
		return nil, filesystem.File{}, err
	}
	if owner == nil {
		return nil, filesystem.File{}, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, ContractView, *owner); err != nil {
		return nil, filesystem.File{}, err
	}
	if s.files == nil {
		return nil, filesystem.File{}, fmt.Errorf("investor: no Disk was configured for the signed contracts (Config.Files)")
	}
	f, err := s.files.Get(ctx, g, record.FileKey)
	if err != nil {
		return nil, filesystem.File{}, err
	}
	return record, f, nil
}
