import { defineConfig, loadEnv } from "vite";
import { transformWithEsbuild } from "vite";
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    plugins: [
      {
        name: "jsx-in-js",
        async transform(code, id) {
          if (/\/src\/.*\.js$/.test(id))
            return transformWithEsbuild(code, id, {
              loader: "jsx",
              jsx: "automatic",
            });
        },
      },
    ],
    define: {
      "process.env.REACT_APP_API_SERVER": JSON.stringify(
        env.VITE_API_SERVER || env.REACT_APP_API_SERVER || "",
      ),
    },
    optimizeDeps: { esbuildOptions: { loader: { ".js": "jsx" } } },
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["./src/testSetup.js"],
      exclude: ["node_modules/**", "e2e/**"],
    },
    build: { outDir: "build" },
  };
});
