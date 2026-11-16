import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { api, type RefreshStatus } from '../api'

const REFRESH_KEY = ['refresh'] as const
/** The latest finished refresh, kept in memory so every page can show it. */
const OUTCOME_KEY = ['refresh-outcome'] as const

/** What a finished refresh found. */
export type RefreshOutcome = RefreshStatus

/**
 * useRefresh drives the "Refresh" button: it starts a poll of every source on
 * the backend, watches it until it finishes, then reloads whatever the pages
 * have on screen.
 *
 * The status is also read on mount, so a refresh started in another tab shows
 * as running here too.
 */
export function useRefresh({
  onFinished,
  pollMs = 1000,
}: {
  onFinished?: (outcome: RefreshOutcome) => void
  pollMs?: number
} = {}) {
  const queryClient = useQueryClient()

  const status = useQuery({
    queryKey: REFRESH_KEY,
    queryFn: api.refreshStatus,
    // Only poll while a refresh is running.
    refetchInterval: (q) => (q.state.data?.running ? pollMs : false),
    retry: false,
    staleTime: 0,
  })

  // Not fetched from anywhere: set when a refresh finishes, cleared on dismiss.
  const outcome = useQuery<RefreshOutcome | null>({
    queryKey: OUTCOME_KEY,
    queryFn: async () => null,
    enabled: false,
    initialData: null,
    staleTime: Infinity,
    gcTime: Infinity,
  })

  const start = useMutation({
    mutationFn: api.startRefresh,
    onSuccess: (data) => queryClient.setQueryData(REFRESH_KEY, data),
  })

  // Notice the running -> finished transition exactly once.
  const wasRunning = useRef(false)
  const finished = useRef(onFinished)
  finished.current = onFinished
  useEffect(() => {
    const data = status.data
    if (!data) return
    if (wasRunning.current && !data.running) {
      queryClient.setQueryData(OUTCOME_KEY, data)
      void queryClient.invalidateQueries({
        predicate: (q) => q.queryKey[0] !== REFRESH_KEY[0] && q.queryKey[0] !== OUTCOME_KEY[0],
      })
      finished.current?.(data)
    }
    wasRunning.current = data.running
  }, [status.data, queryClient])

  return {
    running: start.isPending || status.data?.running === true,
    /** Why a click failed, e.g. the poller isn't running. */
    error: start.error,
    refresh: () => start.mutate(),
    /** What the latest refresh found, until dismissed. Null before any refresh. */
    outcome: outcome.data ?? null,
    dismiss: () => queryClient.setQueryData(OUTCOME_KEY, null),
  }
}
