#!/bin/bash
set -e

# Certificate generation script for Flux Redis cluster
# Generates deterministic certificates using passphrase as seed

CERT_DIR="/etc/ssl/cluster"
CERT_VALIDITY_DAYS=${SSL_CERT_VALIDITY_DAYS:-3650}

echo "=============================================================================="
echo "SSL CERTIFICATE GENERATION"
echo "=============================================================================="
echo "Certificate directory: $CERT_DIR"
echo "Certificate validity: $CERT_VALIDITY_DAYS days"
echo "Time: $(date)"

# Validate required environment variables
if [ -z "$SSL_PASSPHRASE" ]; then
    echo "ERROR: SSL_PASSPHRASE environment variable is required"
    exit 1
fi

if [ -z "$APP_NAME" ]; then
    echo "ERROR: APP_NAME environment variable is required"
    exit 1
fi

if [ -z "$CLUSTER_IPS" ]; then
    echo "ERROR: CLUSTER_IPS environment variable is required"
    exit 1
fi

if [ -z "$MY_IP" ]; then
    echo "ERROR: MY_IP environment variable is required"
    exit 1
fi

echo "Cluster name: $APP_NAME"
echo "Node IP: $MY_IP"
echo "Cluster IPs: $CLUSTER_IPS"

# Create certificate directories
mkdir -p "$CERT_DIR"/{ca,redis,sentinel,proxy}

# Function to generate truly deterministic private key using GnuTLS certtool
generate_deterministic_key() {
    local service=$1
    local keyfile=$2
    local seed="${SSL_PASSPHRASE}-${service}-${APP_NAME}"

    echo "Generating deterministic key for service: $service"

    # Create deterministic seed from passphrase + service + cluster name
    local seed_hex=$(echo -n "$seed" | openssl dgst -sha256 | awk '{print $2}')

    # Validate seed length (should be 64 hex characters = 256 bits)
    if [ ${#seed_hex} -ne 64 ]; then
        echo "Error: Seed generation failed - invalid length: ${#seed_hex}"
        exit 1
    fi

    # Use GnuTLS certtool to generate truly deterministic private key
    if command -v certtool >/dev/null 2>&1; then
        certtool --generate-privkey \
                 --provable \
                 --seed="$seed_hex" \
                 --key-type=rsa \
                 --sec-param=Medium \
                 --outfile "$keyfile" 2>/dev/null || {
            # Fallback to OpenSSL if certtool fails
            local temp_seed_file="/tmp/openssl_seed_$service"
            echo -n "$seed_hex" | xxd -r -p > "$temp_seed_file" 2>/dev/null || echo -n "$seed_hex" > "$temp_seed_file"
            RANDFILE="$temp_seed_file" openssl genrsa -out "$keyfile" 2048 2>/dev/null
            rm -f "$temp_seed_file"
        }
    else
        # Fallback to OpenSSL with seed
        local temp_seed_file="/tmp/openssl_seed_$service"
        echo -n "$seed_hex" | xxd -r -p > "$temp_seed_file" 2>/dev/null || echo -n "$seed_hex" > "$temp_seed_file"
        RANDFILE="$temp_seed_file" openssl genrsa -out "$keyfile" 2048 2>/dev/null
        rm -f "$temp_seed_file"
    fi

    chmod 600 "$keyfile"
    echo "Generated key: $keyfile"
}

# Function to create certificate signing request with proper SANs
create_csr() {
    local service=$1
    local keyfile=$2
    local csrfile=$3
    local cn=$4
    local san_list=""

    # Build Subject Alternative Names for all cluster IPs
    local ip_sans=""
    local dns_sans="DNS:localhost,DNS:${service}"

    for ip in $CLUSTER_IPS; do
        if [ -n "$ip_sans" ]; then
            ip_sans="${ip_sans},"
        fi
        ip_sans="${ip_sans}IP:${ip}"
    done

    # Always include this node's own IP
    if ! echo "$ip_sans" | grep -qF "IP:${MY_IP}"; then
        ip_sans="${ip_sans},IP:${MY_IP}"
    fi

    # Add common localhost addresses
    ip_sans="${ip_sans},IP:127.0.0.1,IP:0.0.0.0"

    san_list="${dns_sans},${ip_sans}"

    # Create certificate signing request
    openssl req -new -key "$keyfile" -out "$csrfile" -subj "/CN=$cn/O=FluxCluster/OU=$APP_NAME" \
        -addext "subjectAltName=$san_list" \
        -addext "keyUsage=digitalSignature,keyEncipherment,keyAgreement" \
        -addext "extendedKeyUsage=serverAuth,clientAuth"
}

# Function to sign certificate with CA
sign_certificate() {
    local csrfile=$1
    local certfile=$2
    local ca_cert=$3
    local ca_key=$4

    # Build proper SAN list for certificate signing
    local dns_sans="DNS:localhost,DNS:redis-server,DNS:redis-sentinel,DNS:redis-proxy"
    local ip_sans=""

    for ip in $CLUSTER_IPS; do
        if [ -n "$ip_sans" ]; then
            ip_sans="${ip_sans},"
        fi
        ip_sans="${ip_sans}IP:${ip}"
    done

    if ! echo "$ip_sans" | grep -qF "IP:${MY_IP}"; then
        ip_sans="${ip_sans},IP:${MY_IP}"
    fi

    ip_sans="${ip_sans},IP:127.0.0.1,IP:0.0.0.0"
    local san_list="${dns_sans},${ip_sans}"

    export SOURCE_DATE_EPOCH=1672531200  # 2023-01-01 00:00:00 UTC

    local serial_seed="${SSL_PASSPHRASE}-${csrfile##*/}-${APP_NAME}"
    local serial_hex=$(echo -n "$serial_seed" | sha256sum | cut -d' ' -f1 | head -c 16)
    local serial_number="0x$serial_hex"

    openssl x509 -req -in "$csrfile" -CA "$ca_cert" -CAkey "$ca_key" \
        -out "$certfile" -days "$CERT_VALIDITY_DAYS" \
        -set_serial "$serial_number" \
        -passin pass:"" \
        -extensions v3_req -extfile <(
        echo "[v3_req]"
        echo "keyUsage = digitalSignature, keyEncipherment, keyAgreement"
        echo "extendedKeyUsage = serverAuth, clientAuth"
        echo "subjectAltName = $san_list"
    )

    chmod 644 "$certfile"
    echo "Signed certificate: $certfile"
}

echo "=============================================================================="
echo "GENERATING ROOT CA"
echo "=============================================================================="

generate_deterministic_key "ca" "$CERT_DIR/ca/ca.key"

export SOURCE_DATE_EPOCH=1672531200

if command -v certtool >/dev/null 2>&1; then
    ca_template="/tmp/ca_template_$$"
    cat > "$ca_template" << EOL
cn = "FluxCluster-CA"
organization = "FluxCluster"
unit = "$APP_NAME"
serial = 1
activation_date = "2023-01-01 00:00:00 UTC"
expiration_date = "2033-01-01 00:00:00 UTC"
ca
cert_signing_key
crl_signing_key
EOL

    certtool --generate-self-signed \
             --load-privkey "$CERT_DIR/ca/ca.key" \
             --template "$ca_template" \
             --outfile "$CERT_DIR/ca/ca.crt" 2>/dev/null || {
        ca_serial_seed="${SSL_PASSPHRASE}-ca-${APP_NAME}"
        ca_serial_hex=$(echo -n "$ca_serial_seed" | sha256sum | cut -d' ' -f1 | head -c 16)
        ca_serial_number="0x$ca_serial_hex"

        openssl req -new -x509 -key "$CERT_DIR/ca/ca.key" -out "$CERT_DIR/ca/ca.crt" \
            -days "$CERT_VALIDITY_DAYS" \
            -set_serial "$ca_serial_number" \
            -subj "/CN=FluxCluster-CA/O=FluxCluster/OU=$APP_NAME" \
            -addext "basicConstraints=CA:TRUE" \
            -addext "keyUsage=keyCertSign,cRLSign" \
            -passin pass:""
    }
    rm -f "$ca_template"
else
    ca_serial_seed="${SSL_PASSPHRASE}-ca-${APP_NAME}"
    ca_serial_hex=$(echo -n "$ca_serial_seed" | sha256sum | cut -d' ' -f1 | head -c 16)
    ca_serial_number="0x$ca_serial_hex"

    openssl req -new -x509 -key "$CERT_DIR/ca/ca.key" -out "$CERT_DIR/ca/ca.crt" \
        -days "$CERT_VALIDITY_DAYS" \
        -set_serial "$ca_serial_number" \
        -subj "/CN=FluxCluster-CA/O=FluxCluster/OU=$APP_NAME" \
        -addext "basicConstraints=CA:TRUE" \
        -addext "keyUsage=keyCertSign,cRLSign" \
        -passin pass:""
fi

chmod 644 "$CERT_DIR/ca/ca.crt"

echo "=============================================================================="
echo "GENERATING REDIS CERTIFICATES"
echo "=============================================================================="

generate_deterministic_key "redis-server" "$CERT_DIR/redis/server.key"
create_csr "redis-server" "$CERT_DIR/redis/server.key" "$CERT_DIR/redis/server.csr" "redis-server"
sign_certificate "$CERT_DIR/redis/server.csr" "$CERT_DIR/redis/server.crt" "$CERT_DIR/ca/ca.crt" "$CERT_DIR/ca/ca.key"
rm "$CERT_DIR/redis/server.csr"

echo "=============================================================================="
echo "GENERATING SENTINEL CERTIFICATES"
echo "=============================================================================="

generate_deterministic_key "redis-sentinel" "$CERT_DIR/sentinel/server.key"
create_csr "redis-sentinel" "$CERT_DIR/sentinel/server.key" "$CERT_DIR/sentinel/server.csr" "redis-sentinel"
sign_certificate "$CERT_DIR/sentinel/server.csr" "$CERT_DIR/sentinel/server.crt" "$CERT_DIR/ca/ca.crt" "$CERT_DIR/ca/ca.key"
rm "$CERT_DIR/sentinel/server.csr"

echo "=============================================================================="
echo "GENERATING PROXY CERTIFICATES"
echo "=============================================================================="

generate_deterministic_key "redis-proxy" "$CERT_DIR/proxy/server.key"
create_csr "redis-proxy" "$CERT_DIR/proxy/server.key" "$CERT_DIR/proxy/server.csr" "redis-proxy"
sign_certificate "$CERT_DIR/proxy/server.csr" "$CERT_DIR/proxy/server.crt" "$CERT_DIR/ca/ca.crt" "$CERT_DIR/ca/ca.key"
rm "$CERT_DIR/proxy/server.csr"

echo "=============================================================================="
echo "SETTING PERMISSIONS"
echo "=============================================================================="

# Set appropriate permissions
chown -R redis:redis "$CERT_DIR/redis" "$CERT_DIR/sentinel" "$CERT_DIR/ca" 2>/dev/null || echo "Warning: Could not set chown redis:redis"

find "$CERT_DIR" -name "*.key" -exec chmod 600 {} \;
find "$CERT_DIR" -name "*.crt" -exec chmod 644 {} \;

echo "Certificate generation completed successfully!"
echo "=============================================================================="
