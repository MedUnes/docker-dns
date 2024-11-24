#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
if [[ "${TRACE-0}" == "1" ]]; then
    set -o xtrace
fi
if [ "$#" -le 2 ]; then
  echo "Expected minimum of 3 arguments, only ${#} passed"
  echo "Usage ./build.sh <github-repo-owner> <repo-name> <repo-version>"
  echo "Ex: ./build.sh medunes docker-dns v1.1.15"
  exit 1;
fi

GITHUB_REPO_OWNER="${1}"
PACKAGE_NAME="${2}"
VERSION="${3}"

NUMERIC_VERSION=$(echo "${VERSION}" | sed  "s/v//g")
BINARY_FOLDER=/usr/bin
CURRENT_DIRECTORY="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY_SOURCE_PATH="${CURRENT_DIRECTORY}"
PACKAGE_PATH="${CURRENT_DIRECTORY}/${PACKAGE_NAME}"

function validateVersion {
  VERSION="${1}"
  if [[ -z "$VERSION" ]]; then
      echo "You have missed to pass a version, aborting!"
      exit 1
  elif [[ "$VERSION" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+)?(\+[0-9A-Za-z-]+)?$ ]]; then
      echo "VERSION=$VERSION"
  else
      echo "$VERSION is not a valid semantic version, aborting.."
      exit 1
  fi
}

function validateBinaryPath {
    BINARY_SOURCE_PATH="${1}"
     if [[ -z "${BINARY_SOURCE_PATH}" ]]; then
         echo "You have missed to pass a version, aborting!"
         exit 1;
     fi
     if [[ $(test -x "${BINARY_SOURCE_PATH}") ]]; then
         echo "Could not find binary at path ${BINARY_SOURCE_PATH}"
         exit 1;
     fi
}
function resetInstallation {
  PACKAGE_NAME="${1}"
  rm -rf "${PACKAGE_NAME}"
  rm -rf "*.deb"
  mkdir -p "${PACKAGE_NAME}"
}
function copySystemdFiles {
    PACKAGE_PATH="${1}"
    CURRENT_DIRECTORY="${2}"
    mkdir -p "${PACKAGE_PATH}"
    cp -r "${CURRENT_DIRECTORY}/lib" "${PACKAGE_PATH}/"
}
function copyConfigFiles {
    PACKAGE_PATH="${1}"
    CURRENT_DIRECTORY="${2}"
    mkdir -p "${PACKAGE_PATH}"
    cp -r "${CURRENT_DIRECTORY}/etc" "${PACKAGE_PATH}/"
}
function copyDebianFiles {
    PACKAGE_PATH="${1}"
    CURRENT_DIRECTORY="${2}"
    PACKAGE_VERSION="${3}"

    mkdir -p "${PACKAGE_PATH}"
    cp -r "${CURRENT_DIRECTORY}/DEBIAN" "${PACKAGE_NAME}"
    sed -i "s#__VERSION__#${PACKAGE_VERSION}#g" "${PACKAGE_NAME}/DEBIAN/control"
    chmod -R 755 "${PACKAGE_NAME}/DEBIAN"
}
function downloadBinary {
  GITHUB_REPO_OWNER="${1}"
  PACKAGE_NAME="${2}"
  VERSION="${3}"
  ARCH=linux_amd64
  NUMERIC_VERSION=$(echo "${VERSION}" | sed "s/v//g")
  BASE_NAME="${PACKAGE_NAME}_${NUMERIC_VERSION}"
  BASE_URL="https://github.com/${GITHUB_REPO_OWNER}/${PACKAGE_NAME}/releases/download/${VERSION}"

  TAR_FILE="${BASE_NAME}_${ARCH}.tar.gz"
  SHA_FILE="${BASE_NAME}_checksums.txt"
  mkdir tmp_download
  cd tmp_download
  echo "$BASE_URL/$TAR_FILE"
  curl -L -o "$TAR_FILE" "$BASE_URL/$TAR_FILE"
  curl -L "$BASE_URL/$SHA_FILE" | grep "${TAR_FILE}" > "${SHA_FILE}"

  sha256sum --check "$SHA_FILE"
  if [ $? -ne 0 ]; then
    echo "Checksum verification failed! for $TAR_FILE using $SHA_FILE"
    exit 1
  fi
  tar -xzvf "$TAR_FILE"
  cd ..
  mv "tmp_download/${PACKAGE_NAME}" "./${PACKAGE_NAME}_tmp"
  rm -rf tmp_download
  ls -al
}
function moveBinaryFile {
  PACKAGE_NAME="${1}"
  DESTINATION="${2}"
  BASE_DIR="${3}"

  mkdir -p "${BASE_DIR}/${PACKAGE_NAME}/${DESTINATION}"
  mv "${BASE_DIR}/${PACKAGE_NAME}_tmp" "${BASE_DIR}/${PACKAGE_NAME}/${DESTINATION}/${PACKAGE_NAME}"
  chmod +x "${BASE_DIR}/${PACKAGE_NAME}/${DESTINATION}/${PACKAGE_NAME}"
}
function generationDebianVersionFile {
  echo "${VERSION}" > "${PACKAGE_PATH}/version"
}
function buildDebianPackage {
  PACKAGE_NAME="${1}"
  VERSION="${2}"
  dpkg-deb --root-owner-group --root-owner-group --build "${PACKAGE_NAME}" "${PACKAGE_NAME}-${VERSION}_amd64.deb"
}

validateVersion "${VERSION}"
validateBinaryPath "${BINARY_SOURCE_PATH}"
resetInstallation "${PACKAGE_NAME}"|| (echo "Could reset installation files & folders" && exit 1)
downloadBinary "${GITHUB_REPO_OWNER}" "${PACKAGE_NAME}" "${VERSION}" || (echo "Could binary file" && exit 1)
moveBinaryFile "${PACKAGE_NAME}" "${BINARY_FOLDER}" "${CURRENT_DIRECTORY}" || (echo "Could not copy binary file" && exit 1)
copySystemdFiles "${PACKAGE_PATH}" "${CURRENT_DIRECTORY}" || (echo "Could not copy&generate systemd files" && exit 1)
copyDebianFiles "${PACKAGE_PATH}" "${CURRENT_DIRECTORY}" "${NUMERIC_VERSION}"|| (echo "Could copy/generate debian files" && exit 1)
copyConfigFiles "${PACKAGE_PATH}" "${CURRENT_DIRECTORY}" "${NUMERIC_VERSION}"|| (echo "Could copy application's config file" && exit 1)
generationDebianVersionFile || (echo "Could generate debian version file" && exit 1)
buildDebianPackage "${PACKAGE_NAME}" "${NUMERIC_VERSION}"|| (echo "Could not build package" && exit 1)
