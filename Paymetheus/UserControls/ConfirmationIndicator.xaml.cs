using Paymetheus.Decred.Wallet;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for TransactionUserControl.xaml
    /// </summary>
    public partial class ConfirmationIndicator : UserControl
    {
        public ConfirmationIndicator()
        {
            this.InitializeComponent();
        }

        public ConfirmationStatus Status
        {
            get { return (ConfirmationStatus)GetValue(StatusProperty); }
            set { SetValue(StatusProperty, value); }
        }

        public static DependencyProperty StatusProperty = DependencyProperty.Register(nameof(Status), typeof(ConfirmationStatus),
            typeof(ConfirmationIndicator), new PropertyMetadata(ConfirmationStatus.Pending, ChangeStatus));

        private static void ChangeStatus(DependencyObject d, DependencyPropertyChangedEventArgs e)
        {
            var control = (ConfirmationIndicator)d;
            var status = (ConfirmationStatus)e.NewValue;
            control.RenderStatus(status);
        }

        private void RenderStatus(ConfirmationStatus status)
        {
            switch (status)
            {
                case ConfirmationStatus.Pending:
                    confirmedSignal.Visibility = Visibility.Collapsed;
                    lblText.Text = "... Pending";
                    lblText.Foreground = (Brush)this.Resources["PendingColor"];
                    rectangle.Stroke = (Brush)this.Resources["PendingColor"];
                    break;

                case ConfirmationStatus.Invalid:
                    confirmedSignal.Visibility = Visibility.Collapsed;
                    lblText.Text = "X  Invalid";
                    lblText.Foreground = (Brush)this.Resources["InvalidColor"];
                    rectangle.Stroke = (Brush)this.Resources["InvalidColor"];
                    break;

                case ConfirmationStatus.Confirmed:
                    confirmedSignal.Visibility = Visibility.Visible;
                    lblText.Text = "    Confirmed";
                    lblText.Foreground = (Brush)this.Resources["ConfirmedColor"];
                    rectangle.Stroke = (Brush)this.Resources["ConfirmedColor"];
                    break;

                default:
                    confirmedSignal.Visibility = Visibility.Collapsed;
                    lblText.Text = "No Status";
                    lblText.Foreground = Brushes.Gray;
                    rectangle.Stroke = Brushes.Gray;
                    break;
            }
        }
    }
}