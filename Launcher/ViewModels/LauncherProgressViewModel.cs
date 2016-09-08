using Paymetheus.Framework;
using System;
using System.Collections.Generic;
using System.IO;
using System.IO.Pipes;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows;

namespace Launcher.ViewModels
{
    public class LauncherProgressViewModel : ViewModelBase
    {
        static string[] statusMessages =
        {
            "Opening database...",
            "Opening ticket database (this may take a while)...",
            "Starting P2P server...",
            "Closing database...",
            "Closing ticket database (this may take a while)...",
            "Stopping P2P server...",
        };

        public LauncherProgressViewModel()
        {
            Task.Run(async () =>
            {
                App.Current.LaunchConcensusServer(out rxPipe, out txPipe);
                await HandleLifetimeEvents();
            }).ContinueWith(App.Current.ExitIfFailed);
        }

        AnonymousPipeServerStream rxPipe = null, txPipe = null;

        private string _currentStatus = "Starting consensus server...";
        public string CurrentStatus
        {
            get { return _currentStatus; }
            set { _currentStatus = value; RaisePropertyChanged(); }
        }

        // If more than just the lifetime event is sent over the rx pipe then
        // the pipe reading will need to be moved elsewhere so it can be shared
        // with other systems.
        private async Task HandleLifetimeEvents()
        {
            var headerBuffer = new byte[1 + 255 + 4]; // max header size
            while (true)
            {
                await rxPipe.ReadAsync(headerBuffer, 0, 1);
                if (headerBuffer[0] != 1)
                {
                    throw new InvalidDataException("Unknown protocol");
                }
                await rxPipe.ReadAsync(headerBuffer, 1, 1);
                await rxPipe.ReadAsync(headerBuffer, 2, headerBuffer[1]);
                var msgType = Encoding.UTF8.GetString(headerBuffer, 2, headerBuffer[1]);
                await rxPipe.ReadAsync(headerBuffer, 2 + headerBuffer[1], 4);
                var payloadSize = ReadUInt32LE(headerBuffer, 2 + headerBuffer[1]);
                var payload = new byte[payloadSize];
                await rxPipe.ReadAsync(payload, 0, (int)payloadSize);

                if (msgType == "lifetimeevent")
                {
                    switch (payload[0])
                    {
                        // Startup event
                        case 0:
                            await App.Current.Dispatcher.InvokeAsync(() =>
                            {
                                CurrentStatus = statusMessages[payload[1]];
                            });
                            break;

                        // Startup completed
                        case 1:
                            // TODO: hide main window
                            await App.Current.Dispatcher.InvokeAsync(() =>
                            {
                                CurrentStatus = "Startup finished";
                                App.Current.LaunchPaymetheus();
                            });
                            break;

                        // Shutdown event
                        case 2:
                            await App.Current.Dispatcher.InvokeAsync(() =>
                            {
                                CurrentStatus = statusMessages[3 + payload[1]];
                            });
                            break;
                    }
                }
            }
        }

        private static uint ReadUInt32LE(byte[] array, int offset)
        {
            return (uint)(array[offset++] | array[offset++] << 8 | array[offset++] << 16 | array[offset] << 24);
        }
    }
}
