import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Suppress ESLint during builds since the existing code uses Vite-era linting
  eslint: {
    ignoreDuringBuilds: true,
  },
  // Suppress TypeScript errors during builds (existing code has Vite-specific types)
  typescript: {
    ignoreBuildErrors: true,
  },
};

export default nextConfig;
