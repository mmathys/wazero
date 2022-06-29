clang -O3 --target=wasm32 --no-standard-libraries -Wl,--export-all -Wl,--no-entry -o $2 $1
