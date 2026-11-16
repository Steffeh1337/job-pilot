package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/matcher"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

// printFunnel writes the per-source funnel for jobs first seen in the last
// days days. Each column is a subset of the one before it.
func printFunnel(ctx context.Context, st *store.Store, days int) error {
	since := time.Now().AddDate(0, 0, -days)
	rows, err := st.Funnel(ctx, since)
	if err != nil {
		return err
	}
	rejects, err := st.RejectReasons(ctx, since)
	if err != nil {
		return err
	}

	fmt.Printf("Funnel: distinct jobs first seen in the last %d day(s)\n\n", days)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "source\tnew\tunique\tregion\trole\tsenior\tb2b\t")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t\n",
			r.Source, r.New, r.Unique, r.Region, r.Role, r.Senior, r.B2B)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if len(rejects) > 0 {
		fmt.Println("\nWhy jobs were dropped:")
		reasons := make([]string, 0, len(rejects))
		for k := range rejects {
			reasons = append(reasons, k)
		}
		sort.Slice(reasons, func(i, j int) bool { return rejects[reasons[i]] > rejects[reasons[j]] })
		for _, k := range reasons {
			fmt.Printf("  %-12s %d\n", k, rejects[k])
		}
	}
	return nil
}

// printCalibration shows how similar the profile is to the indexed jobs and
// the fit settings that follow from it.
func printCalibration(ctx context.Context, m *matcher.Matcher, cfg config.Config) error {
	c, err := m.Calibrate(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Wording similarity between your profile and %d indexed jobs (model %s):\n\n", c.Jobs, c.Model)
	fmt.Printf("  min %.3f   p25 %.3f   median %.3f   p75 %.3f   p95 %.3f   max %.3f\n\n", c.Min, c.P25, c.Median, c.P75, c.P95, c.Max)
	fmt.Println("Suggested settings. A job at or below the low value scores 0% on wording,")
	fmt.Println("and one at or above the high value scores 100%:")
	fmt.Println()
	fmt.Println("  fit:")
	fmt.Printf("    semantic_low: %.2f\n", c.SuggestedLow)
	fmt.Printf("    semantic_high: %.2f\n\n", c.SuggestedHigh)
	fmt.Printf("Currently: %.2f and %.2f.\n", cfg.Fit.SemanticLow, cfg.Fit.SemanticHigh)
	return nil
}
