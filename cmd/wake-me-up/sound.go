package main

import (
	"fmt"
	"os/exec"
)

func playSound(soundFilePath string) {
	var cmd *exec.Cmd

	if _, err := exec.LookPath("afplay"); err == nil {
		// macOS
		cmd = exec.Command("afplay", soundFilePath)
	} else if _, err := exec.LookPath("paplay"); err == nil {
		// Linux with PulseAudio
		cmd = exec.Command("paplay", soundFilePath)
	} else if _, err := exec.LookPath("aplay"); err == nil {
		// Linux with ALSA
		cmd = exec.Command("aplay", soundFilePath)
	} else {
		// Fallback: use system beep
		fmt.Print("\a")
		return
	}

	if cmd != nil {
		if err := cmd.Run(); err != nil {
			log.Printf("Failed to play sound: %v", err)
		}
	}
}
