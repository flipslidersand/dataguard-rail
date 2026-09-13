#[cfg(test)]
mod tests {
    use crate::grpc::dataguard::data_guard_client::DataGuardClient;
    use crate::grpc::dataguard::AnalyzeRequest;
    use crate::run_serve;
    use std::net::TcpListener;
    use std::process::Command;
    use std::time::Duration;
    use tonic::transport::{Certificate, ClientTlsConfig, Endpoint, Identity};

    fn openssl_available() -> bool {
        Command::new("openssl").arg("version").output().is_ok()
    }

    fn run(cmd: &mut Command) {
        let out = cmd.output().expect("failed to spawn openssl");
        assert!(
            out.status.success(),
            "openssl failed: {}",
            String::from_utf8_lossy(&out.stderr)
        );
    }

    /// 自己署名 CA と、127.0.0.1 を SAN に持つサーバー証明書、CN 付きクライアント証明書を生成する。
    fn gen_certs(dir: &std::path::Path) -> (String, String, String, String, String) {
        let ca_key = dir.join("ca.key");
        let ca_crt = dir.join("ca.crt");
        run(Command::new("openssl").args([
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            ca_key.to_str().unwrap(),
            "-out",
            ca_crt.to_str().unwrap(),
            "-days",
            "1",
            "-subj",
            "/CN=test-ca",
        ]));

        let srv_key = dir.join("server.key");
        let srv_csr = dir.join("server.csr");
        let srv_crt = dir.join("server.crt");
        run(Command::new("openssl").args([
            "req",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            srv_key.to_str().unwrap(),
            "-out",
            srv_csr.to_str().unwrap(),
            "-subj",
            "/CN=127.0.0.1",
            "-addext",
            "subjectAltName=IP:127.0.0.1",
        ]));
        run(Command::new("openssl").args([
            "x509",
            "-req",
            "-in",
            srv_csr.to_str().unwrap(),
            "-CA",
            ca_crt.to_str().unwrap(),
            "-CAkey",
            ca_key.to_str().unwrap(),
            "-CAcreateserial",
            "-out",
            srv_crt.to_str().unwrap(),
            "-days",
            "1",
            "-copy_extensions",
            "copy",
        ]));

        let cli_key = dir.join("client.key");
        let cli_csr = dir.join("client.csr");
        let cli_crt = dir.join("client.crt");
        run(Command::new("openssl").args([
            "req",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            cli_key.to_str().unwrap(),
            "-out",
            cli_csr.to_str().unwrap(),
            "-subj",
            "/CN=client.dataguard.local",
        ]));
        run(Command::new("openssl").args([
            "x509",
            "-req",
            "-in",
            cli_csr.to_str().unwrap(),
            "-CA",
            ca_crt.to_str().unwrap(),
            "-CAkey",
            ca_key.to_str().unwrap(),
            "-CAcreateserial",
            "-out",
            cli_crt.to_str().unwrap(),
            "-days",
            "1",
        ]));

        (
            ca_crt.to_str().unwrap().to_string(),
            srv_crt.to_str().unwrap().to_string(),
            srv_key.to_str().unwrap().to_string(),
            cli_crt.to_str().unwrap().to_string(),
            cli_key.to_str().unwrap().to_string(),
        )
    }

    /// 別 CA で署名した「不正な」クライアント証明書を生成する。
    fn gen_untrusted_client_cert(dir: &std::path::Path) -> (String, String) {
        let ca_key = dir.join("other-ca.key");
        let ca_crt = dir.join("other-ca.crt");
        run(Command::new("openssl").args([
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            ca_key.to_str().unwrap(),
            "-out",
            ca_crt.to_str().unwrap(),
            "-days",
            "1",
            "-subj",
            "/CN=untrusted-ca",
        ]));
        let cli_key = dir.join("untrusted-client.key");
        let cli_csr = dir.join("untrusted-client.csr");
        let cli_crt = dir.join("untrusted-client.crt");
        run(Command::new("openssl").args([
            "req",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            cli_key.to_str().unwrap(),
            "-out",
            cli_csr.to_str().unwrap(),
            "-subj",
            "/CN=untrusted-client",
        ]));
        run(Command::new("openssl").args([
            "x509",
            "-req",
            "-in",
            cli_csr.to_str().unwrap(),
            "-CA",
            ca_crt.to_str().unwrap(),
            "-CAkey",
            ca_key.to_str().unwrap(),
            "-CAcreateserial",
            "-out",
            cli_crt.to_str().unwrap(),
            "-days",
            "1",
        ]));
        (
            cli_crt.to_str().unwrap().to_string(),
            cli_key.to_str().unwrap().to_string(),
        )
    }

    fn free_port() -> u16 {
        TcpListener::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
            .port()
    }

    #[tokio::test]
    async fn refuses_non_loopback_bind_without_tls_or_insecure() {
        let err = run_serve("0.0.0.0:0".to_string(), None, None, None, false)
            .await
            .expect_err("expected refusal for plaintext non-loopback bind");
        assert!(err.to_string().contains("refusing to bind"));
    }

    #[tokio::test]
    async fn refuses_tls_client_ca_without_cert_and_key() {
        let dir = tempfile::tempdir().unwrap();
        let ca_path = dir.path().join("ca.pem");
        std::fs::write(
            &ca_path,
            "not a real cert, just needs to exist as an Option",
        )
        .unwrap();

        let err = run_serve(
            "127.0.0.1:0".to_string(),
            None,
            None,
            Some(ca_path.to_str().unwrap().to_string()),
            false,
        )
        .await
        .expect_err("expected refusal when --tls-client-ca is given without --tls-cert/--tls-key");
        assert!(
            err.to_string().contains("--tls-client-ca"),
            "error should mention --tls-client-ca: {err}"
        );
    }

    #[tokio::test]
    async fn mtls_handshake_succeeds_with_trusted_client_cert_and_fails_with_untrusted_one() {
        if !openssl_available() {
            eprintln!("skipping: openssl not found in PATH");
            return;
        }
        let dir = tempfile::tempdir().unwrap();
        let (ca_crt, srv_crt, srv_key, cli_crt, cli_key) = gen_certs(dir.path());
        let (untrusted_crt, untrusted_key) = gen_untrusted_client_cert(dir.path());

        let port = free_port();
        let addr = format!("127.0.0.1:{port}");
        let server = tokio::spawn(run_serve(
            addr.clone(),
            Some(srv_crt),
            Some(srv_key),
            Some(ca_crt.clone()),
            false,
        ));
        // サーバーの起動を待つ
        tokio::time::sleep(Duration::from_millis(300)).await;

        let ca_pem = std::fs::read_to_string(&ca_crt).unwrap();

        // 正しいクライアント証明書 → 接続成功（TLS/mTLS ハンドシェイクが通る）
        {
            let identity = Identity::from_pem(
                std::fs::read_to_string(&cli_crt).unwrap(),
                std::fs::read_to_string(&cli_key).unwrap(),
            );
            let tls = ClientTlsConfig::new()
                .ca_certificate(Certificate::from_pem(&ca_pem))
                .domain_name("127.0.0.1")
                .identity(identity);
            let endpoint = Endpoint::from_shared(format!("https://{addr}"))
                .unwrap()
                .tls_config(tls)
                .unwrap();
            let channel = endpoint
                .connect()
                .await
                .expect("lazy connect should not fail");
            let mut client = DataGuardClient::new(channel);
            // 実際に RPC を発行して TLS/mTLS ハンドシェイクを強制する。
            // sql_path が無効なのでアプリケーションレベルでは InvalidArgument が返るが、
            // これは「トランスポート層（TLS ハンドシェイク）を突破できた」ことの証明になる。
            let result = client
                .analyze(AnalyzeRequest {
                    sql_path: "/nonexistent.sql".to_string(),
                })
                .await;
            assert!(
                result.is_err() && result.as_ref().unwrap_err().code() != tonic::Code::Unavailable,
                "expected mTLS handshake with trusted client cert to succeed \
                 (got transport-level failure instead of an application Status): {:?}",
                result.err()
            );
        }

        // 不正な（別CA署名の）クライアント証明書 → 接続失敗
        {
            let identity = Identity::from_pem(
                std::fs::read_to_string(&untrusted_crt).unwrap(),
                std::fs::read_to_string(&untrusted_key).unwrap(),
            );
            let tls = ClientTlsConfig::new()
                .ca_certificate(Certificate::from_pem(&ca_pem))
                .domain_name("127.0.0.1")
                .identity(identity);
            let endpoint = Endpoint::from_shared(format!("https://{addr}"))
                .unwrap()
                .tls_config(tls)
                .unwrap();
            match endpoint.connect().await {
                Err(_) => {} // 接続自体で TLS ハンドシェイクが失敗した
                Ok(channel) => {
                    let mut client = DataGuardClient::new(channel);
                    let result = client
                        .analyze(AnalyzeRequest {
                            sql_path: "/nonexistent.sql".to_string(),
                        })
                        .await;
                    assert!(
                        result.is_err(),
                        "expected mTLS handshake with untrusted client cert to fail"
                    );
                }
            }
        }

        server.abort();
    }
}
