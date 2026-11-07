package main

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"testing"
)

func TestKNPriceFeedNotRegisteredBeforePublisherParsing(t *testing.T) {
	t.Setenv(apiclientport.EnvPricePublishers, "invalid-publisher-must-not-be-read")
	mod := apiclient.New(nil, clock.System{}, apiclient.Deps{})
	if !assembly.KNPriceProfile.WebsiteDeferred {
		t.Fatal("KN profile must defer website publication")
	}
	if err := wirePricePublishedFeed(mod, nil, clock.System{}, assembly.PriceProfiles()); err != nil {
		t.Fatal(err)
	}
	if len(mod.Service().PublishedSources) != 0 || len(mod.Service().Publishers) != 0 {
		t.Fatal("KN registered a price source with the feed dispatcher")
	}
	other := priceport.Profile{Name: "other_consumer", Roles: map[string]string{"viewer": "TW"}}
	if err := wirePricePublishedFeed(mod, nil, clock.System{}, []priceport.Profile{other}); err == nil {
		t.Fatal("generic profile lost publisher validation")
	}
	t.Setenv(apiclientport.EnvPricePublishers, "89=019a0000-0000-7000-8000-000000000268")
	if err := wirePricePublishedFeed(mod, nil, clock.System{}, []priceport.Profile{other}); err != nil {
		t.Fatal(err)
	}
	if mod.Service().PublishedSources[apiclientport.FeedPriceBoardDaily] == nil || len(mod.Service().Publishers[apiclientport.FeedPriceBoardDaily]) != 1 {
		t.Fatal("generic profile lost its valid feed registration")
	}
}
