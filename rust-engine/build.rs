fn main() -> Result<(), Box<dyn std::error::Error>> {
    // proto/ を rust-engine 内に同梱（crates.io パッケージングで ../proto が使えないため）
    tonic_build::compile_protos("proto/dataguard.proto")?;
    Ok(())
}
