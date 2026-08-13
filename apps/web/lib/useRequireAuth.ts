import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { me } from "@/lib/api";

// 检查登录态；未登录跳 /login。返回 {loggedIn, loading}。
export function useRequireAuth() {
  const router = useRouter();
  const { data, isLoading } = useQuery({ queryKey: ["me"], queryFn: me });
  useEffect(() => {
    if (!isLoading && data && !data.logged_in) router.replace("/login");
  }, [isLoading, data, router]);
  return { loggedIn: !!data?.logged_in, loading: isLoading };
}
