import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { readCloudGuestModels } from './api';

export function useCloudModelCatalog(open: boolean, readSelected = false) {
  const query = useQuery({
    queryKey: ['cloud-guest-models'],
    queryFn: readCloudGuestModels,
    enabled: open || readSelected,
    staleTime: 0,
    refetchInterval: open ? 15_000 : false,
    retry: false,
  });
  const { refetch } = query;
  useEffect(() => {
    if (open) void refetch({ cancelRefetch: false });
  }, [open, refetch]);
  return query;
}
