/*

Package github implements a way to extract
and construct a request to github in order
to retrieve a pom file. If the pom file is
not present, we assume the project is not
build using maven.

*/
package github

import (
	"context"
	"errors"
	"net/http"
	netURL "net/url"
	"strings"

	"github.com/google/go-github/github"
	"github.com/tinakurian/build-tool-detector/controllers/buildtype"
	errs "github.com/tinakurian/build-tool-detector/controllers/error"
	logorus "github.com/tinakurian/build-tool-detector/log"
)

// serviceAttributes used for retrieving
// data using the go-github library.
type requestAttributes struct {
	Owner      string
	Repository string
	Branch     string
}

const (
	master         = "master"
	tree           = "tree"
	slash          = "/"
	pom            = "pom.xml"
	segments       = "segments"
	branch         = "branch"
	attributes     = "attributes"
	url            = "url"
	ghClientID     = "ghClientID"
	ghClientSecret = "ghClientSecret"
)

var (
	// ErrInternalServerErrorFailedContentRetrieval to return if unable to get contents.
	ErrInternalServerErrorFailedContentRetrieval = errors.New("unable to retrieve contents")

	// ErrInternalServerErrorUnsupportedGithubURL BadRequest github url is invalid.
	ErrInternalServerErrorUnsupportedGithubURL = errors.New("unsupported github url")

	// ErrBadRequestInvalidPath BadRequest github url is invalid.
	ErrBadRequestInvalidPath = errors.New("url is invalid")

	// ErrInternalServerErrorUnsupportedService git service unsupported.
	ErrInternalServerErrorUnsupportedService = errors.New("unsupported service")

	// ErrNotFoundResource no resource found.
	ErrNotFoundResource = errors.New("resource not found")

	// FatalLimitedRateLimits github client id and github client secret are unavailable
	FatalLimitedRateLimits = "github client id and github client secret are unavailable"
)

// IGitService git service interface.
type IGitService interface {
	GetContents(ctx context.Context) (*errs.HTTPTypeError, *string)
}

// GitService struct.
type GitService struct {
	ClientID     string
	ClientSecret string
}

// GetContents gets the contents for the service.
func (g GitService) GetContents(ctx context.Context, rawURL string, branchName *string) (*errs.HTTPTypeError, *string) {
	// GetAttributes returns a BadRequest error and
	// will print the error to the user.
	u, err := netURL.Parse(rawURL)
	if err != nil {
		logorus.Logger().
			WithError(err).
			WithField(url, rawURL).
			Warningf(ErrBadRequestInvalidPath.Error())
		return errs.ErrBadRequest(ErrBadRequestInvalidPath), nil
	}

	urlSegments := strings.Split(u.Path, slash)
	httpTypeError, serviceAttribute := getServiceAttributes(urlSegments, branchName)
	if httpTypeError != nil {
		logorus.Logger().
			WithField(segments, urlSegments).
			WithField(branch, branchName).
			Warningf(httpTypeError.Error)
		return httpTypeError, nil
	}

	// getGithubRepositoryPom returns an
	// InternalServerError and will print
	// the buildTool as unknown.
	buildTool := buildtype.UNKNOWN
	httpTypeError = isMaven(ctx, g, serviceAttribute)
	if httpTypeError != nil {
		logorus.Logger().
			WithField(attributes, serviceAttribute).
			Warningf(httpTypeError.Error)
		return httpTypeError, &buildTool
	}

	// Reset the buildToolType to maven since
	// the pom.xml was retrievable.
	buildTool = buildtype.MAVEN

	return nil, &buildTool
}

// getServiceAttributes will use the path segments and
// query params to populate the Attributes
// struct. The attributes struct will be used
// to make a request to github to determine
// the build tool type.
func getServiceAttributes(segments []string, ctxBranch *string) (*errs.HTTPTypeError, requestAttributes) {

	var requestAttrs requestAttributes

	// Default branch that will be used if a branch
	// is not passed in though the optional 'branch'
	// query parameter and is not part of the url.
	branch := master

	if len(segments) <= 2 {
		return errs.ErrBadRequest(ErrBadRequestInvalidPath), requestAttrs
	}

	// If the query parameter field 'branch' is not
	// empty then set the branch name to the query
	// parameter value.
	if ctxBranch != nil {
		branch = *ctxBranch
	} else if len(segments) > 4 {
		// If the user has not specified the branch
		// check whether it is passed in through
		// the URL.
		if segments[3] == tree {
			branch = segments[4]
		}
	}

	requestAttrs = requestAttributes{
		Owner:      segments[1],
		Repository: segments[2],
		Branch:     branch,
	}

	return nil, requestAttrs
}

func isMaven(ctx context.Context, ghService GitService, requestAttrs requestAttributes) *errs.HTTPTypeError {

	// Get the github client id and github client
	// secret if set to get better rate limits.
	t := github.UnauthenticatedRateLimitedTransport{
		ClientID:     ghService.ClientID,
		ClientSecret: ghService.ClientSecret,
	}

	// If the github client id or github client
	// secret are empty, we will log and fail.
	client := github.NewClient(t.Client())
	if t.ClientID == "" || t.ClientSecret == "" {
		logorus.Logger().
			WithField(ghClientID, t.ClientID).
			WithField(ghClientSecret, t.ClientSecret).
			Fatalf(FatalLimitedRateLimits)
	}

	// Check that the repository + branch exists first.
	_, _, err := client.Repositories.GetBranch(ctx, requestAttrs.Owner, requestAttrs.Repository, requestAttrs.Branch)
	if err != nil {
		return errs.ErrNotFoundError(ErrNotFoundResource)
	}

	// If the repository and branch exists, get the contents for the repository.
	_, _, resp, err := client.Repositories.GetContents(
		ctx, requestAttrs.Owner,
		requestAttrs.Repository,
		pom,
		&github.RepositoryContentGetOptions{Ref: requestAttrs.Branch})
	if err != nil && resp.StatusCode != http.StatusOK {
		return errs.ErrInternalServerError(ErrInternalServerErrorFailedContentRetrieval)
	}
	return nil
}
