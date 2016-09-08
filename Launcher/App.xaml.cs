using Launcher.ViewModels;
using Launcher.Views;
using Paymetheus.Decred;
using Paymetheus.Framework;
using Paymetheus.Rpc;
using System;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.IO.Pipes;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Markup;

namespace Launcher
{
    /// <summary>
    /// Interaction logic for App.xaml
    /// </summary>
    public partial class App : Application
    {
        public static new App Current { get; private set; }

        public App()
        {
            if (Current != null)
                throw new ApplicationException("Application instance already exists");

            SingletonViewModelLocator.RegisterFactory<LauncherProgressView, LauncherProgressViewModel>();

            Application.Current.Dispatcher.UnhandledException += (sender, args) =>
            {
                var ex = args.Exception;

                MessageBox.Show(ex.Message, "Error");
                UncleanShutdown();
                Shutdown(1);
            };

            Application.Current.Startup += Application_Startup;

            Current = this;
        }

        private string[] arguments;
        private ProcessArguments parsedArguments;

        private AnonymousPipeServerStream _outPipe = new AnonymousPipeServerStream(PipeDirection.Out, HandleInheritability.Inheritable);
        private AnonymousPipeServerStream _inPipe = new AnonymousPipeServerStream(PipeDirection.In, HandleInheritability.Inheritable);
        private Process _consensusServerProcess;
        private Process _paymetheusProcess;
        private int _consensusServerStarted, _paymetheusProcessStarted; // Atomic
        private int _consensusServerStopped; // Atomic

        private void Application_Startup(object sender, StartupEventArgs e)
        {
            // WPF defaults to using the en-US culture for all formatted bindings.
            // Override this with the system's current culture.
            FrameworkElement.LanguageProperty.OverrideMetadata(typeof(FrameworkElement),
                new FrameworkPropertyMetadata(XmlLanguage.GetLanguage(CultureInfo.CurrentCulture.IetfLanguageTag)));

            arguments = e.Args;
            parsedArguments = ProcessArguments.ParseArguments(e.Args);
            Current.Exit += Application_Exit;
        }

        private void Application_Exit(object sender, ExitEventArgs e)
        {
            CleanShutdown();
        }

        private void SignalConsensusServerShutdown()
        {
            if (Interlocked.Exchange(ref _consensusServerStopped, 1) == 0)
                _outPipe.Dispose();
        }

        private void CleanShutdown()
        {
            SignalConsensusServerShutdown();
            _consensusServerProcess?.WaitForExitAsync();
        }

        private void UncleanShutdown()
        {
            _consensusServerProcess?.KillIfExecuting();
        }

        public void ExitIfFailed(Task t)
        {
            if (t.Exception != null)
            {
                Application.Current.Dispatcher.Invoke(() =>
                {
                    MainWindow.Hide();
                    MessageBox.Show(t.Exception.ToString());
                    UncleanShutdown();
                    Shutdown(1);
                });
            }
        }

        public void LaunchConcensusServer(out AnonymousPipeServerStream rx, out AnonymousPipeServerStream tx)
        {
            if (Interlocked.Exchange(ref _consensusServerStarted, 1) != 0)
                throw new InvalidOperationException("Consensus server already started");

            const string processName = "dcrd";
            var fileName = processName;
            if (!parsedArguments.SearchPathForProcesses)
            {
                fileName = Portability.ExecutableInstallationPath(Environment.OSVersion.Platform, "Decred", processName);
            }

            var procInfo = new ProcessStartInfo
            {
                FileName = fileName,
                Arguments = $"--piperx {_outPipe.GetClientHandleAsString()} --pipetx {_inPipe.GetClientHandleAsString()} --lifetimeevents",
                CreateNoWindow = parsedArguments.IntendedNetwork == BlockChainIdentity.MainNet, // only hide on mainnet
                UseShellExecute = false,
            };
            if (parsedArguments.IntendedNetwork != BlockChainIdentity.MainNet)
            {
                procInfo.Arguments += $" --{parsedArguments.IntendedNetwork.Name}";
            }

            _consensusServerProcess = Process.Start(procInfo);

            rx = _inPipe;
            tx = _outPipe;

            Task.Run(async () =>
            {
                var exitCode = await _consensusServerProcess.WaitForExitAsync();
                if (exitCode != 0)
                {
                    MessageBox.Show($"Consensus server exited with code {exitCode}");
                }
                await Application.Current.Dispatcher.InvokeAsync(Application.Current.Shutdown);
            });
        }

        public void LaunchPaymetheus()
        {
            if (Interlocked.Exchange(ref _paymetheusProcessStarted, 1) != 0)
                throw new InvalidOperationException("Paymetheus already started");

            const string processName = "Paymetheus";
            var fileName = processName;
            if (!parsedArguments.SearchPathForProcesses)
            {
                fileName = Portability.ExecutableInstallationPath(Environment.OSVersion.Platform, "Decred", processName);
            }

            var procInfo = new ProcessStartInfo
            {
                FileName = fileName,
                Arguments = string.Join(" ", arguments), // pass arguments vertabim
                UseShellExecute = false,
            };
            _paymetheusProcess = Process.Start(procInfo);

            // For now only hide the launcher when running on mainnet.
            if (parsedArguments.IntendedNetwork == BlockChainIdentity.MainNet)
            {
                App.Current.Dispatcher.Invoke(() =>
                {
                    App.Current.MainWindow.Hide();
                });
            }

            Task.Run(async () =>
            {
                await _paymetheusProcess.WaitForExitAsync();
                if (parsedArguments.IntendedNetwork == BlockChainIdentity.MainNet)
                {
                    await App.Current.Dispatcher.InvokeAsync(() =>
                    {
                        App.Current.MainWindow.Show();
                    });
                }
                SignalConsensusServerShutdown();
            });
        }
    }
}
