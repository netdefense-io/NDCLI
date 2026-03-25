package vpn

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// FetchNetwork fetches a single VPN network.
func FetchNetwork(ctx context.Context, client *api.Client, org, vpnName string) (*models.VpnNetwork, error) {
	resp, err := client.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s",
		url.PathEscape(org), url.PathEscape(vpnName)), nil)
	if err != nil {
		return nil, err
	}
	var network models.VpnNetwork
	if err := api.ParseResponse(resp, &network); err != nil {
		return nil, err
	}
	return &network, nil
}

// FetchMember fetches a single VPN member.
func FetchMember(ctx context.Context, client *api.Client, org, vpnName, device string) (*models.VpnMember, error) {
	resp, err := client.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members/%s",
		url.PathEscape(org), url.PathEscape(vpnName), url.PathEscape(device)), nil)
	if err != nil {
		return nil, err
	}
	var member models.VpnMember
	if err := api.ParseResponse(resp, &member); err != nil {
		return nil, err
	}
	return &member, nil
}

// FetchAllMembers fetches all VPN members with pagination.
func FetchAllMembers(ctx context.Context, client *api.Client, org, vpnName string) ([]models.VpnMember, error) {
	var all []models.VpnMember
	page := 1
	perPage := 500

	for {
		params := map[string]string{
			"page":     strconv.Itoa(page),
			"per_page": strconv.Itoa(perPage),
		}
		resp, err := client.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/members",
			url.PathEscape(org), url.PathEscape(vpnName)), params)
		if err != nil {
			return nil, err
		}
		var result models.VpnMemberListResponse
		if err := api.ParseResponse(resp, &result); err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if len(all) >= result.Total {
			break
		}
		page++
	}

	return all, nil
}

// FetchAllLinks fetches all VPN links with pagination.
func FetchAllLinks(ctx context.Context, client *api.Client, org, vpnName string) ([]models.VpnLink, error) {
	var all []models.VpnLink
	page := 1
	perPage := 500

	for {
		params := map[string]string{
			"page":     strconv.Itoa(page),
			"per_page": strconv.Itoa(perPage),
		}
		resp, err := client.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/vpn-networks/%s/links",
			url.PathEscape(org), url.PathEscape(vpnName)), params)
		if err != nil {
			return nil, err
		}
		var result models.VpnLinkListResponse
		if err := api.ParseResponse(resp, &result); err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if len(all) >= result.Total {
			break
		}
		page++
	}

	return all, nil
}
