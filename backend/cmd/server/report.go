package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

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
	fmt.Fprintln(w, "source\tnew\tunique\tregion\trole\tstack\tsenior\tb2b\t")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t\n",
			r.Source, r.New, r.Unique, r.Region, r.Role, r.Stack, r.Senior, r.B2B)
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
