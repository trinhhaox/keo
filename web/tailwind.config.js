/** @type {import('tailwindcss').Config} */
export default {
  // Quét mọi class tĩnh trong HTML + JSX để JIT sinh đúng utilities dùng thật.
  content: ["./index.html", "./src/**/*.{js,jsx}"],
  theme: {
    extend: {},
  },
  plugins: [],
};
