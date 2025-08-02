package copier

import (
	"fmt"
	"os"
	"time"
)

// logf logs a message using the custom logger when progress bar is active,
// or the standard logger otherwise
func (c *Copier) logf(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		colorize(ColorDim, timestamp),
		formatLogLevel("INFO"),
		message)

	if c.logger != nil {
		// For progress bar mode, just write the formatted message
		c.logger.SetFlags(0) // Remove default timestamp since we add our own
		c.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// logSuccess logs a success message with green color and checkmark
func (c *Copier) logSuccess(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		colorize(ColorDim, timestamp),
		formatLogLevel("SUCCESS"),
		message)

	if c.logger != nil {
		c.logger.SetFlags(0)
		c.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// logWarn logs a warning message with yellow color and warning icon
func (c *Copier) logWarn(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		colorize(ColorDim, timestamp),
		formatLogLevel("WARN"),
		message)

	if c.logger != nil {
		c.logger.SetFlags(0)
		c.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// logError logs an error message with red color and error icon
func (c *Copier) logError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		colorize(ColorDim, timestamp),
		formatLogLevel("ERROR"),
		message)

	if c.logger != nil {
		c.logger.SetFlags(0)
		c.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}

// logProgress logs a progress message with cyan color and progress icon
func (c *Copier) logProgress(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")

	formattedMsg := fmt.Sprintf("%s %s %s",
		colorize(ColorDim, timestamp),
		formatLogLevel("INFO"),
		message)

	if c.logger != nil {
		c.logger.SetFlags(0)
		c.logger.Print(formattedMsg)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", formattedMsg)
	}
}
