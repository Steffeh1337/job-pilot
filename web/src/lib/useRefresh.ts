import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { api, type RefreshStatus } from '../api'

const REFRESH_KEY = ['refresh'] as const

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
      void queryClient.invalidateQueries({ predicate: (q) => q.queryKey[0] !== REFRESH_KEY[0] })
      finished.current?.(data)
    }
    wasRunning.current = data.running
  }, [status.data, queryClient])

  return {
    running: start.isPending || status.data?.running === true,
    /** Why a click failed, e.g. the poller isn't running. */
    error: start.error,
    refresh: () => start.mutate(),
  }
}
