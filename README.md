# File Upload Server

A simple file upload server that generates random download URLs with a 7-day expiration period.

## Features

- File upload with size limit (100MB)
- Random download URL generation
- 7-day file expiration
- Automatic cleanup of expired files
- Modern web interface

## Requirements

- Go 1.21 or later
- Required Go packages:
  - github.com/gin-gonic/gin
  - github.com/google/uuid

## Installation

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Run the server:
   ```bash
   go run main.go
   ```

The server will start on port 8080. Visit http://localhost:8080 to access the web interface.

## Usage

1. Open the web interface in your browser
2. Click "Choose File" to select a file to upload
3. Click "Upload" to upload the file
4. After successful upload, you'll receive a download link
5. The download link will be valid for 7 days

## Security Notes

- Files are stored with random UUID names
- Original filenames are preserved for downloads
- Files are automatically deleted after 7 days
- Maximum file size is limited to 100MB